package server

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/krillinai/Clawee/server/internal/mcpgateway"
	"github.com/krillinai/Clawee/server/internal/rbac"
)

func mountModelActivityRoutes(admin *gin.RouterGroup, opts Options) {
	for _, path := range []string{"/activity/model-overview", "/activity/model-requests", "/activity/model-requests/detail"} {
		admin.GET(path, requirePermission(opts.RBACService, rbac.PermissionActivityRead), handleModelActivity(opts))
	}
}

func modelActivityQuery(c *gin.Context, opts Options) (mcpgateway.ModelActivityFilter, gin.H, error) {
	f := mcpgateway.ModelActivityFilter{Page: 1, PageSize: 20, Limit: 20, Now: time.Now().UTC()}
	r, err := opts.ActivityService.DateRange(c.Query("range"))
	if err != nil {
		return f, nil, invalidActivityParameter("range")
	}
	f.Start, f.End = r.Start, r.End
	for _, field := range []struct {
		name  string
		value *string
	}{{"user_id", &f.UserID}, {"agent_id", &f.AgentID}, {"session_id", &f.SessionID}, {"turn_id", &f.TurnID}} {
		raw := c.Query(field.name)
		if raw != "" {
			*field.value = modelActivityID(raw)
			if *field.value == "" {
				return f, nil, invalidActivityParameter(field.name)
			}
		}
	}
	if raw := c.Query("unattributed"); raw != "" {
		f.Unattributed, err = strconv.ParseBool(raw)
		if err != nil {
			return f, nil, invalidActivityParameter("unattributed")
		}
	}
	if (f.Unattributed && f.AgentID != "") || (f.SessionID != "" && (f.UserID == "" || f.AgentID == "")) || (f.TurnID != "" && (f.UserID == "" || f.AgentID == "" || f.SessionID == "")) {
		return f, nil, invalidActivityParameter("主体筛选")
	}
	for _, field := range []struct {
		name  string
		value *int
		max   int
	}{{"page", &f.Page, 1000000}, {"page_size", &f.PageSize, 100}, {"limit", &f.Limit, 100}} {
		if raw := c.Query(field.name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > field.max {
				return f, nil, invalidActivityParameter(field.name)
			}
			*field.value = n
		}
	}
	cursorTime, cursorID := c.Query("cursor_created_at"), c.Query("cursor_id")
	if (cursorTime == "") != (cursorID == "") {
		return f, nil, invalidActivityParameter("cursor")
	}
	if cursorTime != "" {
		f.CursorCreatedAt, err = time.Parse(time.RFC3339Nano, cursorTime)
		f.CursorID = strings.TrimSpace(cursorID)
		if err != nil || f.CursorID == "" || len(f.CursorID) > 256 {
			return f, nil, invalidActivityParameter("cursor")
		}
	}
	meta := modelActivityMeta()
	meta["range"], meta["timezone"], meta["start"], meta["end"] = r.Range, r.Timezone, r.Start, r.End
	return f, meta, nil
}

func modelActivityMeta() gin.H { return gin.H{"source": "model_proxy", "schema_version": 1} }

func handleModelActivity(opts Options) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := currentAccount(c); !ok {
			activityError(c, 401, "unauthorized", "未登录")
			return
		}
		if modelAccessMode(opts) != ModelAccessPlatformManaged {
			activityError(c, 503, "model_activity_unavailable", "当前模型调用未经过 Gateway，模型活动不可用")
			return
		}
		if opts.ProxyGateway == nil {
			activityError(c, 503, "model_activity_unavailable", "模型活动暂不可用")
			return
		}
		store, ok := opts.ProxyGateway.Store().(mcpgateway.ModelActivityStore)
		if !ok {
			activityError(c, 503, "model_activity_unavailable", "模型活动暂不可用")
			return
		}
		if strings.HasSuffix(c.Request.URL.Path, "/detail") {
			if len(c.Request.URL.Query()) != 1 || c.Query("audit_id") == "" || len(c.Query("audit_id")) > 256 {
				activityError(c, 400, "invalid_request", "请求标识无效")
				return
			}
			record, err := store.GetModelActivityRecord(c.Request.Context(), c.Query("audit_id"))
			if errors.Is(err, mcpgateway.ErrModelProxyAuditNotFound) {
				activityError(c, 404, "model_request_not_found", "未找到该模型调用")
				return
			}
			if err != nil {
				activityError(c, 503, "model_activity_unavailable", "模型活动暂不可用")
				return
			}
			data := modelActivityRequest(c, opts, record)
			data["activity_content"] = record.ActivityContent
			data["meta"] = modelActivityMeta()
			c.JSON(200, gin.H{"data": data})
			return
		}
		if opts.ActivityService == nil {
			activityError(c, 503, "model_activity_unavailable", "模型活动暂不可用")
			return
		}
		filter, meta, err := modelActivityQuery(c, opts)
		if err != nil {
			activityError(c, 400, "invalid_request", err.Error())
			return
		}
		if strings.HasSuffix(c.Request.URL.Path, "model-overview") {
			result, err := store.ModelActivityOverview(c.Request.Context(), filter)
			if err != nil {
				activityError(c, 503, "model_activity_unavailable", "模型活动暂不可用")
				return
			}
			for i := range result.Agents {
				a := &result.Agents[i]
				a.UserName, a.AgentName = modelActivityNames(c, opts, a.UserID, a.AgentID, "")
			}
			meta["page"], meta["page_size"], meta["total"] = filter.Page, filter.PageSize, result.Total
			c.JSON(200, gin.H{"data": gin.H{"summary": result.Summary, "agents": result.Agents, "meta": meta}})
			return
		}
		page, err := store.ListModelActivityRecords(c.Request.Context(), filter)
		if err != nil {
			activityError(c, 503, "model_activity_unavailable", "模型活动暂不可用")
			return
		}
		items := make([]gin.H, 0, len(page.Items))
		for _, record := range page.Items {
			data := modelActivityRequest(c, opts, record)
			content := record.ActivityContent
			input, _ := limitActivityText(content.InputText, 160)
			output, _ := limitActivityText(content.OutputText, 160)
			data["input_preview"], data["output_preview"] = input, output
			data["input_status"], data["output_status"] = content.InputStatus, content.OutputStatus
			data["input_truncated"], data["output_truncated"] = content.InputTruncated, content.OutputTruncated
			items = append(items, data)
		}
		var next *time.Time
		if !page.NextCreatedAt.IsZero() {
			next = &page.NextCreatedAt
		}
		meta["limit"], meta["next_created_at"], meta["next_id"] = filter.Limit, next, page.NextID
		c.JSON(200, gin.H{"data": gin.H{"items": items, "meta": meta}})
	}
}

func modelActivityNames(c *gin.Context, opts Options, userID, agentID, snapshot string) (string, string) {
	userName := snapshot
	if userName == "" {
		userName = userID
	}
	if opts.AccountService != nil {
		if account, err := opts.AccountService.Account(c.Request.Context(), userID); err == nil {
			userName = account.DisplayName()
		}
	}
	agentName := "未关联智能体"
	if agentID != "" {
		agentName = agentID
		if agent, err := opts.ProxyGateway.Store().GetAgent(c.Request.Context(), agentID); err == nil && strings.TrimSpace(agent.Name) != "" {
			agentName = agent.Name
		}
	}
	return userName, agentName
}

func modelActivityRequest(c *gin.Context, opts Options, r mcpgateway.ModelProxyAuditRecord) gin.H {
	userName, agentName := modelActivityNames(c, opts, r.UserID, r.AgentID, r.UserNameSnapshot)
	var completedAt *time.Time
	var duration *int64
	var tokens *int64
	if r.Status != mcpgateway.ModelProxyAuditStarted {
		duration = &r.DurationMS
		if !r.CompletedAt.IsZero() {
			completedAt = &r.CompletedAt
		}
	}
	usageStatus := mcpgateway.ModelProxyAuditUsageStatus(r)
	if usageStatus != mcpgateway.ModelProxyUsageUnavailable {
		tokens = &r.TotalTokens
	}
	return gin.H{"audit_id": r.AuditID, "user_id": r.UserID, "user_name": userName, "agent_id": r.AgentID, "agent_name": agentName, "session_id": r.SessionID, "turn_id": r.TurnID, "model": r.Model,
		"created_at": r.CreatedAt, "completed_at": completedAt, "status": r.Status, "error_code": r.ErrorCode, "total_tokens": tokens, "usage_status": usageStatus, "duration_ms": duration}
}
