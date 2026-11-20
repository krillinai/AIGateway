package server

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/krillinai/Clawee/server/internal/accounts"
	"github.com/krillinai/Clawee/server/internal/activity"
	"github.com/krillinai/Clawee/server/internal/mcpgateway"
	"github.com/krillinai/Clawee/server/internal/rbac"
)

func TestModelActivityRoutesRequireActivityPermission(t *testing.T) {
	ctx := context.Background()
	accountStore := accounts.NewMemoryStore()
	_ = accountStore.SaveAccount(ctx, accounts.Account{UserID: "reader", Status: accounts.StatusActive})
	accountService := accounts.NewService(accounts.Config{Store: accountStore})
	rbacStore := rbac.NewMemoryStore()
	rbacService := rbac.NewService(rbac.Config{Store: rbacStore, Accounts: accountService})
	if err := rbacService.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	store := mcpgateway.NewMemoryStore()
	_ = store.CreateModelProxyAuditRecord(ctx, mcpgateway.ModelProxyAuditRecord{AuditID: "p2", Method: "POST", Endpoint: "/v1/responses", UserID: "reader", CreatedAt: time.Now(), Status: mcpgateway.ModelProxyAuditStarted, ActivityContent: &mcpgateway.ModelActivityContent{SchemaVersion: 1}})
	activityService, _ := activity.NewService(nil, nil, nil, "Asia/Shanghai")
	for _, tc := range []struct {
		name              string
		login, permission bool
		mode              ModelAccessMode
		want              int
	}{{"未登录", false, false, "", 401}, {"缺少权限", true, false, "", 403}, {"仅活动权限", true, true, "", 200}, {"未经过Gateway", true, true, ModelAccessEnterpriseManaged, 503}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.permission {
				_ = rbacStore.EnsureSystemRole(ctx, rbac.Role{RoleID: "activity", Code: "activity_reader", Name: "活动查看", Permissions: []string{rbac.PermissionActivityRead}})
				_ = rbacStore.AssignAccountRole(ctx, rbac.AccountRole{UserID: "reader", RoleID: "activity"}, rbac.OperationAudit{})
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tc.login {
					c.Set(accountContextKey, accounts.Account{UserID: "reader", Status: accounts.StatusActive})
				}
			})
			mountModelActivityRoutes(router.Group("/api/v1/admin"), Options{AccountService: accountService, RBACService: rbacService, ActivityService: activityService, ModelAccessMode: tc.mode, ProxyGateway: mcpgateway.NewService(mcpgateway.Config{Store: store})})
			for _, path := range []string{"model-overview", "model-requests", "model-requests/detail?audit_id=p2"} {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/admin/activity/"+path, nil))
				if rec.Code != tc.want {
					t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
				}
			}
		})
	}
}
