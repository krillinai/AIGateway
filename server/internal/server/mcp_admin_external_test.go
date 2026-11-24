package server

import (
	"context"
	"testing"
	"time"

	"github.com/krillinai/Clawee/server/internal/accounts"
	"github.com/krillinai/Clawee/server/internal/mcpgateway"
)

func TestExternalAgentListItemsIncludesModelClients(t *testing.T) {
	ctx := context.Background()
	accountStore := accounts.NewMemoryStore()
	if err := accountStore.SaveAccount(ctx, accounts.Account{
		UserID: "user_external",
		Email:  "external@example.com",
		Status: accounts.StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	accountService := accounts.NewService(accounts.Config{Store: accountStore})
	modelStore := mcpgateway.NewMemoryStore()
	now := time.Now().UTC()
	for _, clientType := range []string{mcpgateway.ModelActivityClientCodex, mcpgateway.ModelActivityClientWorkBuddy} {
		if err := modelStore.CreateModelProxyAuditRecord(ctx, mcpgateway.ModelProxyAuditRecord{
			AuditID:         "audit_" + clientType,
			UserID:          "user_external",
			ClientType:      clientType,
			Endpoint:        "/v1/responses",
			Method:          "POST",
			Status:          mcpgateway.ModelProxyAuditSucceeded,
			CreatedAt:       now.Add(-time.Hour),
			ActivityContent: &mcpgateway.ModelActivityContent{SchemaVersion: 1},
		}); err != nil {
			t.Fatal(err)
		}
	}

	items := externalAgentListItems(ctx, Options{
		AccountService: accountService,
		ProxyGateway:   mcpgateway.NewService(mcpgateway.Config{Store: modelStore}),
	}, "")
	if len(items) != 2 {
		t.Fatalf("external items = %d, want 2", len(items))
	}
	for _, item := range items {
		if item["External"] != true || item["BoundUserName"] != "external@example.com" || item["BoundUserID"] != "user_external" {
			t.Fatalf("external item = %#v", item)
		}
		if item["CreationSource"] != mcpgateway.AgentCreationSourceExternal {
			t.Fatalf("creation source = %#v", item["CreationSource"])
		}
	}
}
