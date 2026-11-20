package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/krillinai/Clawee/server/internal/clawadmin"
	"github.com/krillinai/Clawee/server/internal/mcpgateway"
)

func TestModelActivityProxyNonStreamingContent(t *testing.T) {
	message := map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "Bearer abcdefghijklmnop"}}}
	for _, tc := range []struct {
		name, status, outputStatus string
		large, disconnected        bool
	}{
		{"大响应", "completed", "available", true, false},
		{"协议内部分输出", "incomplete", "partial", false, false},
		{"协议内失败", "failed", "partial", false, false},
		{"响应中断", "completed", "partial", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := []any{message}
			if tc.large {
				output = append(output, map[string]any{"type": "function_call", "arguments": strings.Repeat("x", 8<<20)})
			}
			body, err := json.Marshal(map[string]any{"status": tc.status, "output": output, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.disconnected {
				body = body[:strings.Index(string(body), `"usage":`)+len(`"usage":`)]
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tc.disconnected {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)+100))
				}
				_, _ = w.Write(body)
			}))
			defer upstream.Close()
			router, store := modelProxyTestRouter(t, fixedModelConfiguration{configuration: clawadmin.ModelConfiguration{
				BaseURL: upstream.URL + "/v1", APIKey: "synthetic-upstream-key", Model: "test-model",
			}})
			req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"input":"测试"}`))
			req.Header.Set("Authorization", "Bearer model-token")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != 200 || rec.Body.String() != string(body) {
				t.Fatalf("采集改变了转发响应: status=%d bytes=%d", rec.Code, rec.Body.Len())
			}
			page, err := store.ListModelActivityRecords(context.Background(), mcpgateway.ModelActivityFilter{Limit: 20})
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("审计记录错误: count=%d err=%v", len(page.Items), err)
			}
			record := page.Items[0]
			content := record.ActivityContent
			if content.OutputText != "Bearer [REDACTED]" || content.OutputStatus != tc.outputStatus || content.OutputTruncated {
				t.Fatalf("非流式内容错误: %+v", content)
			}
			if tc.disconnected {
				if record.Status != mcpgateway.ModelProxyAuditFailed || record.ErrorCode != "upstream_error" {
					t.Fatalf("中断审计状态错误: status=%q error=%q", record.Status, record.ErrorCode)
				}
			} else if record.Status != mcpgateway.ModelProxyAuditSucceeded || record.TotalTokens != 2 || record.UsageStatus != mcpgateway.ModelProxyUsageComplete {
				t.Fatalf("原审计状态或用量被改变: status=%q tokens=%d usage=%q", record.Status, record.TotalTokens, record.UsageStatus)
			}
		})
	}
}

func TestModelActivityProxyLifecycle(t *testing.T) {
	const response = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Bearer abcdefghijklmnop\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Claw-Agent-ID") != "" || r.Header.Get("X-Claw-Session-ID") != "" || r.Header.Get("X-Claw-Turn-ID") != "" {
			t.Error("关联 Header 泄漏到上游")
		}
		if r.Header.Get("X-Test-Associated") == "true" && r.Header.Get("x-codex-turn-metadata") == "" {
			t.Error("原生 metadata 未透传")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(response))
	}))
	defer upstream.Close()
	router, store := modelProxyTestRouter(t, fixedModelConfiguration{configuration: clawadmin.ModelConfiguration{
		BaseURL: upstream.URL + "/v1", APIKey: "synthetic-upstream-key", Model: "test-model",
	}})
	for _, turn := range []string{"first", "first", "first", "second", ""} {
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"input":"Bearer abcdefghijklmnop","stream":true}`))
		req.Header.Set("Authorization", "Bearer model-token")
		req.Header.Set("Content-Type", "application/json")
		if turn != "" {
			req.Header.Set("X-Claw-Agent-ID", "agent_model")
			req.Header.Set("X-Claw-Session-ID", "local-session")
			req.Header.Set("X-Claw-Turn-ID", "stale-legacy")
			req.Header.Set("x-codex-turn-metadata", `{"turn_id":"`+turn+`"}`)
			req.Header.Set("X-Test-Associated", "true")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != 200 || rec.Body.String() != response {
			t.Fatalf("代理响应被采集改变: status=%d", rec.Code)
		}
	}
	ctx := context.Background()
	filter := mcpgateway.ModelActivityFilter{Start: time.Now().Add(-time.Hour), End: time.Now().Add(time.Hour), Now: time.Now(), Page: 1, PageSize: 20, Limit: 20}
	result, err := store.ModelActivityOverview(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.RequestCount != 5 || result.Summary.SessionCount != 1 || result.Summary.TurnCount != 2 || result.Summary.TotalTokens == nil || *result.Summary.TotalTokens != 10 {
		t.Fatalf("代理活动口径错误: %+v", result.Summary)
	}
	page, err := store.ListModelActivityRecords(ctx, filter)
	if err != nil || len(page.Items) != 5 {
		t.Fatalf("审计记录数量错误: %v", err)
	}
	for _, record := range page.Items {
		content := record.ActivityContent
		if record.Status != mcpgateway.ModelProxyAuditSucceeded || content.InputText != "Bearer [REDACTED]" || content.OutputText != "Bearer [REDACTED]" || content.OutputStatus != "available" {
			t.Fatalf("终态或脱敏未保存: %+v %+v", record, content)
		}
		if record.AgentID == "" && (record.SessionID != "" || record.TurnID != "") {
			t.Fatal("旧客户端关联被猜测绑定")
		}
		if record.AgentID != "" && (record.AgentID != "agent_model" || record.SessionID != "local-session" || record.TurnID == "stale-legacy") {
			t.Fatal("请求关联未按原生轮次保存")
		}
	}
}
