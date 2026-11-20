package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/krillinai/Clawee/server/internal/mcpgateway"
	"github.com/krillinai/Clawee/server/internal/office/claweeactivity"
)

const modelActivityTextLimit = 8000
const modelActivityEventLimit = 64 << 10

func modelActivityID(value string) string {
	if !utf8.ValidString(value) {
		return ""
	}
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > 128 {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return value
}

func modelActivityHeader(h http.Header, name string) string {
	values := h.Values(name)
	if len(values) != 1 {
		return ""
	}
	return modelActivityID(values[0])
}

func modelActivityTurnID(h http.Header) string {
	values, exists := h[http.CanonicalHeaderKey("x-codex-turn-metadata")]
	if !exists {
		return modelActivityHeader(h, "X-Claw-Turn-ID")
	}
	if len(values) != 1 || len(values[0]) > 8<<10 || !utf8.ValidString(values[0]) {
		return ""
	}
	var value map[string]json.RawMessage
	if json.Unmarshal([]byte(values[0]), &value) != nil || value == nil {
		return ""
	}
	var id string
	if json.Unmarshal(value["turn_id"], &id) != nil {
		return ""
	}
	return modelActivityID(id)
}

func bindModelActivity(c *gin.Context, opts Options, record *mcpgateway.ModelProxyAuditRecord, payload map[string]any) {
	if c.Request.Method != http.MethodPost || (record.Endpoint != "/v1/responses" && record.Endpoint != "/v1/chat/completions") {
		return
	}
	record.ActivityContent = &mcpgateway.ModelActivityContent{SchemaVersion: 1, InputStatus: "unavailable", OutputStatus: "pending"}
	input := modelActivityInput(record.Endpoint, payload)
	record.ActivityContent.InputText, record.ActivityContent.InputTruncated = modelActivityText(input)
	if record.ActivityContent.InputText != "" {
		record.ActivityContent.InputStatus = "available"
	}
	agentID := modelActivityHeader(c.Request.Header, "X-Claw-Agent-ID")
	if agentID == "" || opts.AccountService == nil || opts.ProxyGateway == nil {
		return
	}
	owner, err := opts.AccountService.AccountForAgent(c.Request.Context(), agentID)
	if err != nil || owner.UserID != record.UserID {
		return
	}
	agent, err := opts.ProxyGateway.Store().GetAgent(c.Request.Context(), agentID)
	if err != nil || agent.Status != mcpgateway.StatusActive {
		return
	}
	record.AgentID = agentID
	record.SessionID = modelActivityHeader(c.Request.Header, "X-Claw-Session-ID")
	if record.SessionID != "" {
		record.TurnID = modelActivityTurnID(c.Request.Header)
	}
}

func limitActivityText(text string, limit int) (string, bool) {
	text = strings.ToValidUTF8(text, "")
	count := 0
	for index := range text {
		if count == limit {
			return text[:index], true
		}
		count++
	}
	return text, false
}

func modelActivityText(text string) (string, bool) {
	text, truncated := limitActivityText(text, modelActivityTextLimit)
	text, redactedTruncated := limitActivityText(claweeactivity.RedactText(text), modelActivityTextLimit)
	return text, truncated || redactedTruncated
}

func readableActivityText(value any, allowedType string) string {
	if text, ok := value.(string); ok {
		return text
	}
	blocks, _ := value.([]any)
	var text strings.Builder
	for _, value := range blocks {
		block, _ := value.(map[string]any)
		kind, _ := block["type"].(string)
		if kind == allowedType || (allowedType == "input_text" && kind == "text") {
			if part, ok := block["text"].(string); ok {
				text.WriteString(part)
			}
		}
	}
	return text.String()
}

func modelActivityInput(endpoint string, payload map[string]any) string {
	field, textType := "input", "input_text"
	if endpoint == "/v1/chat/completions" {
		field, textType = "messages", "text"
	}
	if endpoint == "/v1/responses" {
		if text, ok := payload[field].(string); ok {
			return text
		}
	}
	messages, _ := payload[field].([]any)
	for i := len(messages) - 1; i >= 0; i-- {
		message, _ := messages[i].(map[string]any)
		if message["role"] == "user" {
			return readableActivityText(message["content"], textType)
		}
	}
	return ""
}

type modelActivityBlockKey struct{ Output, Content int }
type modelActivityCapture struct {
	stream     bool
	responses  bool
	pending    []byte
	previousCR bool
	event      []byte
	eventBytes int
	skipping   bool
	truncated  bool
	ended      bool
	failed     bool
	blocks     map[modelActivityBlockKey]string
	size       int
}

func (a *modelActivityCapture) setBlock(key modelActivityBlockKey, text string, appendText bool) {
	if a.blocks == nil {
		a.blocks = make(map[modelActivityBlockKey]string)
	}
	previous := a.blocks[key]
	if appendText {
		text = previous + text
	}
	remaining := modelActivityTextLimit - a.size + utf8.RuneCountInString(previous)
	bounded, truncated := limitActivityText(text, remaining)
	a.truncated = a.truncated || truncated
	a.size += utf8.RuneCountInString(bounded) - utf8.RuneCountInString(previous)
	if bounded != "" {
		a.blocks[key] = bounded
	} else {
		delete(a.blocks, key)
	}
}

func (a *modelActivityCapture) Consume(chunk []byte) {
	if !a.stream {
		// JSON 响应使用独立缓冲，不依赖审计快照；超限不会影响转发。
		const maxJSON = 8 << 20
		remaining := maxJSON - len(a.pending)
		if len(chunk) > remaining {
			a.truncated = true
			chunk = chunk[:remaining]
		}
		a.pending = append(a.pending, chunk...)
		return
	}
	for _, b := range chunk {
		if a.previousCR && b == '\n' {
			a.previousCR = false
			continue
		}
		a.previousCR = b == '\r'
		if b == '\n' || b == '\r' {
			line := a.pending
			if len(line) > 0 {
				a.eventBytes++
				if a.eventBytes > modelActivityEventLimit {
					a.truncated = true
					a.skipping = true
				}
			}
			if len(line) == 0 {
				if !a.skipping {
					a.consumeEvent(a.event)
				}
				a.event = a.event[:0]
				a.eventBytes = 0
				a.skipping = false
			} else if !a.skipping && bytes.HasPrefix(line, []byte("data:")) {
				data := bytes.TrimPrefix(line, []byte("data:"))
				data = bytes.TrimPrefix(data, []byte(" "))
				if len(a.event) > 0 {
					a.event = append(a.event, '\n')
				}
				a.event = append(a.event, data...)
			}
			a.pending = a.pending[:0]
		} else {
			a.eventBytes++
			if a.eventBytes > modelActivityEventLimit {
				a.truncated = true
				a.skipping = true
			}
			// 保留一个字节，区分被丢弃的超长行与空行。
			if !a.skipping || len(a.pending) == 0 {
				a.pending = append(a.pending, b)
			}
		}
	}
}

func activityIndex(value any) int { number, _ := value.(float64); return int(number) }

func (a *modelActivityCapture) consumeEvent(data []byte) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
		a.ended = true
		return
	}
	var value map[string]any
	if json.Unmarshal(data, &value) != nil {
		return
	}
	if !a.responses {
		choices, _ := value["choices"].([]any)
		for _, item := range choices {
			choice, _ := item.(map[string]any)
			if activityIndex(choice["index"]) != 0 {
				continue
			}
			delta, _ := choice["delta"].(map[string]any)
			if text, ok := delta["content"].(string); ok {
				a.setBlock(modelActivityBlockKey{}, text, true)
			}
		}
		if value["error"] != nil {
			a.failed = true
		}
		return
	}
	kind, _ := value["type"].(string)
	key := modelActivityBlockKey{activityIndex(value["output_index"]), activityIndex(value["content_index"])}
	switch kind {
	case "response.output_text.delta":
		if text, ok := value["delta"].(string); ok {
			a.setBlock(key, text, true)
		}
	case "response.output_text.done":
		if text, ok := value["text"].(string); ok {
			a.setBlock(key, text, false)
		}
	case "response.completed":
		response, _ := value["response"].(map[string]any)
		a.readResponseOutput(response)
		a.ended = true
	case "error", "response.failed", "response.incomplete":
		a.failed = true
	}
}

func (a *modelActivityCapture) readResponseOutput(value map[string]any) {
	output, _ := value["output"].([]any)
	for i, item := range output {
		message, _ := item.(map[string]any)
		if message["role"] != "assistant" || message["type"] != "message" {
			continue
		}
		content, _ := message["content"].([]any)
		for j, item := range content {
			block, _ := item.(map[string]any)
			if block["type"] == "output_text" {
				if text, ok := block["text"].(string); ok {
					a.setBlock(modelActivityBlockKey{i, j}, text, false)
				}
			}
		}
	}
}

func (a *modelActivityCapture) Finalize(content *mcpgateway.ModelActivityContent, transportOK bool) {
	if content == nil {
		return
	}
	if !a.stream {
		var value map[string]any
		if json.Unmarshal(a.pending, &value) == nil {
			if a.responses {
				a.readResponseOutput(value)
			} else {
				choices, _ := value["choices"].([]any)
				if len(choices) > 0 {
					choice, _ := choices[0].(map[string]any)
					message, _ := choice["message"].(map[string]any)
					a.setBlock(modelActivityBlockKey{}, readableActivityText(message["content"], "text"), false)
				}
			}
			a.ended = true
		}
	}
	keys := make([]modelActivityBlockKey, 0, len(a.blocks))
	for key := range a.blocks {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Output == keys[j].Output {
			return keys[i].Content < keys[j].Content
		}
		return keys[i].Output < keys[j].Output
	})
	var text strings.Builder
	for _, key := range keys {
		text.WriteString(a.blocks[key])
	}
	content.OutputText, content.OutputTruncated = modelActivityText(text.String())
	content.OutputTruncated = content.OutputTruncated || a.truncated
	content.OutputStatus = "unavailable"
	if content.OutputText != "" {
		content.OutputStatus = "partial"
		if transportOK && a.ended && !a.failed {
			content.OutputStatus = "available"
		}
	}
}

// 保持标识校验错误与模型执行解耦。
func invalidActivityParameter(name string) error { return fmt.Errorf("参数 %s 无效", name) }
