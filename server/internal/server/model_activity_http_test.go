package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/krillinai/Clawee/server/internal/activity"
	"github.com/krillinai/Clawee/server/internal/mcpgateway"
	"github.com/krillinai/Clawee/server/internal/server"
)

func TestModelActivityAPIContracts(t *testing.T) {
	store := mcpgateway.NewMemoryStore()
	activityService, _ := activity.NewService(nil, nil, nil, "Asia/Shanghai")
	router := newTestRouter(t, server.Options{ProxyGateway: testProxyGateway(store), ActivityService: activityService})
	now := time.Now().UTC()
	content := &mcpgateway.ModelActivityContent{SchemaVersion: 1, InputText: "用户文本", OutputText: "助手文本", InputStatus: "available", OutputStatus: "available"}
	record := mcpgateway.ModelProxyAuditRecord{AuditID: "p2", UserID: testAdminUserID(t, router), Endpoint: "/v1/responses", Method: "POST", CreatedAt: now, Status: mcpgateway.ModelProxyAuditStarted, ActivityContent: content, APIKeyID: "must_not_return", RequestHeaders: mcpgateway.JSONMap{"secret": "must_not_return"}, RequestBody: mcpgateway.JSONMap{"raw": "must_not_return"}}
	if err := store.CreateModelProxyAuditRecord(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	old := record
	old.AuditID = "legacy"
	old.ActivityContent = nil
	_ = store.CreateModelProxyAuditRecord(context.Background(), old)
	for _, tc := range []struct {
		path string
		code int
	}{
		{"model-overview", 200}, {"model-requests", 200}, {"model-requests/detail?audit_id=p2", 200},
		{"model-overview?range=bad", 400}, {"model-overview?page=0", 400}, {"model-overview?page_size=101", 400},
		{"model-requests?cursor_id=one", 400}, {"model-requests?cursor_created_at=2026-01-01T00:00:00Z", 400},
		{"model-requests?session_id=s", 400}, {"model-requests?user_id=u&agent_id=a&turn_id=t", 400},
		{"model-overview?agent_id=a&unattributed=true", 400}, {"model-requests?limit=101", 400},
		{"model-requests/detail?audit_id=legacy", 404}, {"model-requests/detail?audit_id=missing", 404},
		{"model-requests/detail?audit_id=p2&range=today", 400},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/activity/"+tc.path, nil))
			if rec.Code != tc.code {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "must_not_return") {
				t.Fatal("返回原始审计敏感字段")
			}
			if tc.code == 200 {
				var response struct {
					Data map[string]any `json:"data"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(tc.path, "model-requests/detail") {
					if response.Data["completed_at"] != nil || response.Data["duration_ms"] != nil || response.Data["total_tokens"] != nil || response.Data["session_id"] != "" {
						t.Fatalf("空值契约错误: %#v", response.Data)
					}
				}
			}
		})
	}
	// 详情与时间范围无关，历史采集记录仍可直达。
	historical := record
	historical.AuditID = "historical"
	historical.CreatedAt = now.AddDate(-1, 0, 0)
	_ = store.CreateModelProxyAuditRecord(context.Background(), historical)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/admin/activity/model-requests/detail?audit_id=historical", nil))
	if rec.Code != 200 {
		t.Fatal("详情错误地依赖时间范围")
	}
}
