package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/krillinai/Clawee/server/internal/accounts"
	"github.com/krillinai/Clawee/server/internal/mcpgateway"
)

func TestModelActivityTurnHeaders(t *testing.T) {
	for _, tc := range []struct {
		name   string
		native []string
		want   string
	}{
		{"兼容回退", nil, "legacy"}, {"原生优先", []string{`{"turn_id":" native ","thread_id":"ignored"}`}, "native"},
		{"非法不回退", []string{`{"turn_id":42}`}, ""}, {"空对象", []string{`{}`}, ""}, {"重复", []string{`{"turn_id":"a"}`, `{"turn_id":"b"}`}, ""},
		{"超限", []string{strings.Repeat(" ", 8193)}, ""}, {"非法UTF8", []string{"\xff"}, ""}, {"超长ID", []string{`{"turn_id":"` + strings.Repeat("中", 129) + `"}`}, ""},
		{"控制字符", []string{`{"turn_id":"a\nb"}`}, ""}, {"非对象", []string{`null`}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			h.Set("X-Claw-Turn-ID", "legacy")
			for _, value := range tc.native {
				h.Add("x-codex-turn-metadata", value)
			}
			if got := modelActivityTurnID(h); got != tc.want {
				t.Fatalf("turn=%q want=%q", got, tc.want)
			}
		})
	}
	source := http.Header{"X-Claw-Agent-Id": []string{"a"}, "X-Claw-Session-Id": []string{"s"}, "X-Claw-Turn-Id": []string{"t"}, "X-Codex-Turn-Metadata": []string{`{"turn_id":"n"}`}}
	dest := http.Header{}
	copyModelProxyRequestHeaders(dest, source)
	if dest.Get("X-Claw-Agent-ID") != "" || dest.Get("X-Claw-Session-ID") != "" || dest.Get("X-Claw-Turn-ID") != "" || dest.Get("x-codex-turn-metadata") == "" {
		t.Fatal("Header 转发规则错误")
	}
}

func TestBindModelActivityUsesCodexMetadataWithoutAgentHeader(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("x-codex-turn-metadata", `{"thread_id":"thread-codex","turn_id":"turn-codex"}`)
	record := mcpgateway.ModelProxyAuditRecord{UserID: "user", Endpoint: "/v1/responses"}
	bindModelActivity(c, Options{}, &record, map[string]any{"input": "hello"})
	if record.ClientType != mcpgateway.ModelActivityClientCodex || record.AgentID != "" || record.SessionID != "thread-codex" || record.TurnID != "turn-codex" {
		t.Fatalf("Codex 关联错误: %+v", record)
	}
	if record.SessionSource != "codex_thread_id" || record.TurnSource != "codex_turn_id" {
		t.Fatalf("Codex 关联来源错误: %+v", record)
	}
}

func TestBindModelActivityUsesGenericHeadersWithoutAgentHeader(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Client-Session-ID", "session-generic")
	c.Request.Header.Set("X-Client-Turn-ID", "turn-generic")
	record := mcpgateway.ModelProxyAuditRecord{UserID: "user", Endpoint: "/v1/chat/completions"}
	bindModelActivity(c, Options{}, &record, map[string]any{"messages": []any{}})
	if record.ClientType != mcpgateway.ModelActivityClientGeneric || record.SessionID != "session-generic" || record.TurnID != "turn-generic" {
		t.Fatalf("通用客户端关联错误: %+v", record)
	}
}

func TestModelActivityInputAndCapture(t *testing.T) {
	for _, tc := range []struct{ endpoint, body, want string }{
		{"/v1/responses", `{"input":"你好"}`, "你好"},
		{"/v1/responses", `{"input":[{"role":"user","content":[{"type":"input_text","text":"旧"}]},{"role":"assistant","content":"忽略"},{"role":"user","content":[{"type":"input_image","image_url":"data:secret"}]}]}`, ""},
		{"/v1/chat/completions", `{"messages":[{"role":"system","content":"指令"},{"role":"user","content":[{"type":"text","text":"用户"},{"type":"image_url","image_url":"data:secret"}]}]}`, "用户"},
		{"/v1/responses", `{"previous_response_id":"old","input":[{"type":"function_call_output","output":"工具正文"}]}`, ""},
	} {
		var payload map[string]any
		_ = json.Unmarshal([]byte(tc.body), &payload)
		if got := modelActivityInput(tc.endpoint, payload); got != tc.want {
			t.Fatalf("input=%q", got)
		}
	}
	for _, tc := range []struct {
		name, body, want, status     string
		stream, responses, transport bool
	}{
		{"ChatJSON", `{"choices":[{"message":{"role":"assistant","content":"你好"}}]}`, "你好", "available", false, false, true},
		{"ResponsesJSON", `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"回答"}]}]}`, "回答", "available", false, true, true},
		{"ResponsesJSON部分输出", `{"status":"incomplete","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"部分回答"}]}]}`, "部分回答", "partial", false, true, true},
		{"ResponsesJSON失败输出", `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"部分回答"}]}],"status":"failed"}`, "部分回答", "partial", false, true, true},
		{"ResponsesJSON失败无文本", `{"status":"failed","output":[]}`, "", "unavailable", false, true, true},
		{"ResponsesJSON尾部断开", `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"已收到的文本"}]}],"usage":`, "已收到的文本", "partial", false, true, false},
		{"ResponsesJSON文本块后断开", `{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Bearer abcdefghijklmnop"},{"type":"output_text","text":"未收完`, "Bearer [REDACTED]", "partial", false, true, false},
		{"ChatJSON消息中断开", `{"choices":[{"message":{"role":"assistant","content":"已收到的文本"`, "已收到的文本", "partial", false, false, false},
		{"JSON缺少结尾", `{"choices":[{"message":{"content":"已收到的文本"}}]`, "已收到的文本", "partial", false, false, true},
		{"JSON尾部非法", `{"choices":[{"message":{"content":"已收到的文本"}}]}invalid`, "已收到的文本", "partial", false, false, true},
		{"JSON嵌套过深", `{"choices":[{"message":{"content":"已收到的文本"}}],"extra":` + strings.Repeat("[", 10001) + "0" + strings.Repeat("]", 10001) + "}", "已收到的文本", "partial", false, false, true},
		{"ChatSSE", "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你好\"}}]}\r\n\r\ndata: [DONE]\n\n", "你好", "available", true, false, true},
		{"CR换行", "data: {\"choices\":[{\"delta\":{\"content\":\"答复\"}}]}\r\rdata: [DONE]\r\r", "答复", "available", true, false, true},
		{"缺失终态", "data: {\"choices\":[{\"delta\":{\"content\":\"部分\"}}]}\n\n", "部分", "partial", true, false, true},
		{"断开", "data: {\"choices\":[{\"delta\":{\"content\":\"部分\"}}]}\n\ndata: [DONE]\n\n", "部分", "partial", true, false, false},
		{"重复全文", "data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"你好\"}\n\ndata: {\"type\":\"response.output_text.done\",\"text\":\"你好\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"你好\"}]}]}}\n\n", "你好", "available", true, true, true},
		{"协议内错误", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"部分\"}\n\ndata: {\"type\":\"response.failed\"}\n\ndata: [DONE]\n\n", "部分", "partial", true, true, true},
		{"空全文校准", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"旧内容\"}\n\ndata: {\"type\":\"response.output_text.done\",\"text\":\"\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{}}\n\n", "", "unavailable", true, true, true},
		{"工具响应", `{"output":[{"type":"function_call","name":"exec","call_id":"call_1","arguments":"机密"}]}`, "", "available", false, true, true},
		{"跨分片脱敏", "data: {\"choices\":[{\"delta\":{\"content\":\"Bearer abcdefghijklmnop\"}}]}\n\ndata: [DONE]\n\n", "Bearer [REDACTED]", "available", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, size := range []int{1, 7, 32768} {
				capture := modelActivityCapture{stream: tc.stream, responses: tc.responses}
				usage := modelProxyUsageAccumulator{jsonResponse: !tc.stream}
				raw := []byte(tc.body)
				for len(raw) > 0 {
					n := min(size, len(raw))
					usage.Consume(raw[:n])
					capture.Consume(raw[:n])
					raw = raw[n:]
				}
				content := mcpgateway.ModelActivityContent{}
				capture.Finalize(&content, tc.transport, usage.pending)
				if content.OutputText != tc.want || content.OutputStatus != tc.status {
					t.Fatalf("size=%d content=%+v", size, content)
				}
			}
		})
	}
	toolCapture := modelActivityCapture{responses: true}
	toolContent := mcpgateway.ModelActivityContent{}
	toolCapture.Finalize(&toolContent, true, []byte(`{"status":"completed","output":[{"type":"function_call","name":"exec","call_id":"call_1","arguments":"Bearer abcdefghijklmnop"}]}`))
	if len(toolContent.ToolCalls) != 1 || toolContent.ToolCalls[0].Name != "exec" || toolContent.ToolCalls[0].InputText != "Bearer [REDACTED]" || toolContent.OutputStatus != "available" {
		t.Fatalf("工具调用解析错误: %+v", toolContent)
	}
	text, truncated := modelActivityText(strings.Repeat("中", 8001))
	if utf8.RuneCountInString(text) != 8000 || !truncated || !utf8.ValidString(text) {
		t.Fatal("Unicode 截断错误")
	}
	capture := modelActivityCapture{stream: true}
	capture.Consume([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"已取得\"}}]}\n\ndata: " + strings.Repeat("x", 65537) + "\n\ndata: [DONE]\n\n"))
	content := mcpgateway.ModelActivityContent{}
	capture.Finalize(&content, true, nil)
	if content.OutputText != "已取得" || !content.OutputTruncated {
		t.Fatalf("超限事件丢失已有文本: %+v", content)
	}
}

func TestModelActivityLargeJSON(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
		largeTool        bool
		truncated        bool
	}{
		{"大响应中的短文本", "短回答", "短回答", true, false},
		{"超长文本保留摘录", strings.Repeat("中", (8<<20)/3+1), strings.Repeat("中", modelActivityTextLimit), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": tc.text}}}}
			if tc.largeTool {
				output = append(output, map[string]any{"type": "function_call", "arguments": strings.Repeat("x", 8<<20)})
			}
			raw, err := json.Marshal(map[string]any{"status": "completed", "output": output})
			if err != nil {
				t.Fatal(err)
			}
			capture := modelActivityCapture{responses: true}
			usage := modelProxyUsageAccumulator{jsonResponse: true}
			for offset := 0; offset < len(raw); offset += 32768 {
				usage.Consume(raw[offset:min(offset+32768, len(raw))])
				capture.Consume(raw[offset:min(offset+32768, len(raw))])
			}
			content := mcpgateway.ModelActivityContent{}
			capture.Finalize(&content, true, usage.pending)
			if content.OutputText != tc.want || content.OutputStatus != "available" || content.OutputTruncated != tc.truncated {
				t.Fatalf("大响应摘录错误: status=%q truncated=%v text_chars=%d", content.OutputStatus, content.OutputTruncated, utf8.RuneCountInString(content.OutputText))
			}
		})
	}
}

func TestEnrichModelActivityContentFromResponseBody(t *testing.T) {
	content := &mcpgateway.ModelActivityContent{OutputStatus: "unavailable"}
	enrichModelActivityContentFromResponseBody(content, mcpgateway.JSONMap{
		"content_type": "text/event-stream",
		"events": []any{
			map[string]any{"type": "response.output_item.added", "output_index": float64(0), "item": map[string]any{"type": "custom_tool_call", "id": "tool_1", "call_id": "call_1", "name": "exec"}},
			map[string]any{"type": "response.custom_tool_call_input.delta", "item_id": "tool_1", "delta": "{\"cmd\":\"pwd\"}"},
		},
	})
	if len(content.ToolCalls) != 1 || content.ToolCalls[0].Name != "exec" || content.ToolCalls[0].InputText != `{"cmd":"pwd"}` {
		t.Fatalf("历史响应工具调用解析错误: %+v", content)
	}
	prefix, err := json.Marshal([]any{map[string]any{"type": "response.output_item.done", "output_index": float64(0), "item": map[string]any{"type": "function_call", "id": "tool_2", "call_id": "call_2", "name": "exec", "arguments": "{\"cmd\":\"ls\"}"}}})
	if err != nil {
		t.Fatal(err)
	}
	prefixContent := &mcpgateway.ModelActivityContent{}
	enrichModelActivityContentFromResponseBody(prefixContent, mcpgateway.JSONMap{"prefix": string(prefix)})
	if len(prefixContent.ToolCalls) != 1 || prefixContent.ToolCalls[0].CallID != "call_2" || prefixContent.ToolCalls[0].InputText != `{"cmd":"ls"}` {
		t.Fatalf("截断响应前缀工具调用解析错误: %+v", prefixContent)
	}
}

func TestBindModelActivityRequiresOwnedActiveAgent(t *testing.T) {
	ctx := context.Background()
	accountStore := accounts.NewMemoryStore()
	_ = accountStore.SaveAccount(ctx, accounts.Account{UserID: "user", Status: accounts.StatusActive})
	// 复用账户关联，不从 metadata 建立归属。
	if err := accountStore.SaveAccountAgent(ctx, accounts.AccountAgent{UserID: "user", AgentID: "agent"}); err != nil {
		t.Fatal(err)
	}
	store := mcpgateway.NewMemoryStore()
	_ = store.SaveAgent(ctx, mcpgateway.AgentRegistration{AgentID: "agent", Status: mcpgateway.StatusActive})
	opts := Options{AccountService: accounts.NewService(accounts.Config{Store: accountStore}), ProxyGateway: mcpgateway.NewService(mcpgateway.Config{Store: store})}
	for _, user := range []string{"user", "other"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		c.Request.Header.Set("X-Claw-Agent-ID", "agent")
		c.Request.Header.Set("X-Claw-Session-ID", "local")
		c.Request.Header.Set("x-codex-turn-metadata", `{"turn_id":"turn","thread_id":"runtime"}`)
		record := mcpgateway.ModelProxyAuditRecord{UserID: user, Endpoint: "/v1/responses"}
		bindModelActivity(c, opts, &record, map[string]any{"input": "文本"})
		if user == "user" {
			if record.AgentID != "agent" || record.SessionID != "local" || record.TurnID != "turn" {
				t.Fatalf("绑定失败: %+v", record)
			}
		} else if record.AgentID != "" || record.SessionID != "" || record.TurnID != "" {
			t.Fatal("越权绑定")
		}
	}
}
