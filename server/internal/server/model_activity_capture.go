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

func modelActivityTurnMetadata(h http.Header) (sessionID, turnID string, present bool) {
	values, exists := h[http.CanonicalHeaderKey("x-codex-turn-metadata")]
	if !exists {
		return "", "", false
	}
	if len(values) != 1 || len(values[0]) > 8<<10 || !utf8.ValidString(values[0]) {
		return "", "", true
	}
	var value map[string]json.RawMessage
	if json.Unmarshal([]byte(values[0]), &value) != nil || value == nil {
		return "", "", true
	}
	var session, turn string
	if json.Unmarshal(value["thread_id"], &session) == nil {
		sessionID = modelActivityID(session)
	}
	if json.Unmarshal(value["turn_id"], &turn) == nil {
		turnID = modelActivityID(turn)
	}
	return sessionID, turnID, true
}

func modelActivityTurnID(h http.Header) string {
	_, turnID, present := modelActivityTurnMetadata(h)
	if !present {
		return modelActivityHeader(h, "X-Claw-Turn-ID")
	}
	return turnID
}

func modelActivityClientHint(h http.Header) string {
	value := strings.ToLower(modelActivityHeader(h, "X-Client-Type"))
	switch value {
	case mcpgateway.ModelActivityClientClaude:
		return mcpgateway.ModelActivityClientClaude
	case mcpgateway.ModelActivityClientGeneric:
		return mcpgateway.ModelActivityClientGeneric
	case mcpgateway.ModelActivityClientCodex:
		return mcpgateway.ModelActivityClientCodex
	}
	userAgent := strings.ToLower(h.Get("User-Agent"))
	if strings.Contains(userAgent, "claude-code") || strings.Contains(userAgent, "claude-cli") || strings.Contains(userAgent, "claude_cli") {
		return mcpgateway.ModelActivityClientClaude
	}
	return ""
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
	record.ClientType = mcpgateway.ModelActivityClientUnknown
	agentID := modelActivityHeader(c.Request.Header, "X-Claw-Agent-ID")
	agentHeaderPresent := len(c.Request.Header.Values("X-Claw-Agent-ID")) > 0
	agentValid := false
	if agentID != "" && opts.AccountService != nil && opts.ProxyGateway != nil {
		owner, err := opts.AccountService.AccountForAgent(c.Request.Context(), agentID)
		if err == nil && owner.UserID == record.UserID {
			agent, agentErr := opts.ProxyGateway.Store().GetAgent(c.Request.Context(), agentID)
			agentValid = agentErr == nil && agent.Status == mcpgateway.StatusActive
		}
	}
	if agentValid {
		record.AgentID = agentID
		record.ClientType = mcpgateway.ModelActivityClientClawee
	}

	metadataSession, metadataTurn, metadataPresent := modelActivityTurnMetadata(c.Request.Header)
	if !agentHeaderPresent && metadataPresent {
		record.ClientType = mcpgateway.ModelActivityClientCodex
	}
	if !agentHeaderPresent && !metadataPresent {
		if hint := modelActivityClientHint(c.Request.Header); hint != "" {
			record.ClientType = hint
		} else if modelActivityHeader(c.Request.Header, "X-Client-Session-ID") != "" || modelActivityHeader(c.Request.Header, "X-Client-Turn-ID") != "" {
			record.ClientType = mcpgateway.ModelActivityClientGeneric
		}
	}
	if agentValid {
		if sessionID := modelActivityHeader(c.Request.Header, "X-Claw-Session-ID"); sessionID != "" {
			record.SessionID, record.SessionSource = sessionID, "clawee_header"
		}
	}
	if record.SessionID == "" && (!agentHeaderPresent || agentValid) {
		if metadataSession != "" {
			record.SessionID, record.SessionSource = metadataSession, "codex_thread_id"
		} else if sessionID := modelActivityHeader(c.Request.Header, "X-Client-Session-ID"); sessionID != "" {
			record.SessionID, record.SessionSource = sessionID, "client_header"
		}
	}
	if metadataPresent && (!agentHeaderPresent || agentValid) {
		if metadataTurn != "" {
			record.TurnID, record.TurnSource = metadataTurn, "codex_turn_id"
		}
	} else if agentValid {
		if turnID := modelActivityHeader(c.Request.Header, "X-Claw-Turn-ID"); turnID != "" {
			record.TurnID, record.TurnSource = turnID, "clawee_header"
		}
	} else if turnID := modelActivityHeader(c.Request.Header, "X-Client-Turn-ID"); turnID != "" {
		record.TurnID, record.TurnSource = turnID, "client_header"
	}
	if record.TurnID != "" && record.SessionID == "" {
		record.TurnID, record.TurnSource = "", ""
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

// 逐项解码；后续 JSON 不完整时，仍返回已解码的字段和文本块。
func decodeModelActivityJSONValue(decoder *json.Decoder, depth int) (any, error) {
	// 与 encoding/json 的最大嵌套深度保持一致。
	if depth > 10000 {
		return nil, fmt.Errorf("JSON 嵌套层数过多")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		value := make(map[string]any)
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return value, err
			}
			field, err := decodeModelActivityJSONValue(decoder, depth+1)
			value[key.(string)] = field
			if err != nil {
				return value, err
			}
		}
		_, err := decoder.Token()
		return value, err
	case json.Delim('['):
		value := make([]any, 0)
		for decoder.More() {
			item, err := decodeModelActivityJSONValue(decoder, depth+1)
			value = append(value, item)
			if err != nil {
				return value, err
			}
		}
		_, err := decoder.Token()
		return value, err
	default:
		return token, nil
	}
}

func (a *modelActivityCapture) Finalize(content *mcpgateway.ModelActivityContent, transportOK bool, responseJSON []byte) {
	if content == nil {
		return
	}
	if !a.stream {
		decoded, err := decodeModelActivityJSONValue(json.NewDecoder(bytes.NewReader(responseJSON)), 0)
		value, _ := decoded.(map[string]any)
		if a.responses {
			a.readResponseOutput(value)
			a.failed = value["status"] == "incomplete" || value["status"] == "failed"
		} else {
			choices, _ := value["choices"].([]any)
			if len(choices) > 0 {
				choice, _ := choices[0].(map[string]any)
				message, _ := choice["message"].(map[string]any)
				a.setBlock(modelActivityBlockKey{}, readableActivityText(message["content"], "text"), false)
			}
		}
		a.ended = err == nil && json.Valid(responseJSON)
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
