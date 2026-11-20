package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/krillinai/Clawee/server/internal/mcpgateway"
)

func TestModelActivityPostgresMatchesMemory(t *testing.T) {
	pool, _ := skillRoleTestDatabase(t, 91)
	ctx := context.Background()
	postgres := NewMCPGatewayStore(pool)
	memory := mcpgateway.NewMemoryStore()
	now := time.Now().UTC().Truncate(time.Microsecond)
	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	records := []mcpgateway.ModelProxyAuditRecord{}
	for i := 0; i < 3; i++ {
		records = append(records, mcpgateway.ModelProxyAuditRecord{AuditID: fmt.Sprintf("same-turn-%d", i), UserID: "u1", AgentID: "a1", SessionID: "same", TurnID: "same", TotalTokens: 10, UsageStatus: mcpgateway.ModelProxyUsageComplete})
	}
	records = append(records,
		mcpgateway.ModelProxyAuditRecord{AuditID: "next-turn", UserID: "u1", AgentID: "a1", SessionID: "same", TurnID: "next", UsageStatus: mcpgateway.ModelProxyUsagePartial, TotalTokens: 5, Status: mcpgateway.ModelProxyAuditFailed},
		mcpgateway.ModelProxyAuditRecord{AuditID: "other-user", UserID: "u2", AgentID: "a2", SessionID: "same", TurnID: "same"},
		mcpgateway.ModelProxyAuditRecord{AuditID: "unattributed", UserID: "u1"},
		mcpgateway.ModelProxyAuditRecord{AuditID: "started", UserID: "u1", Status: mcpgateway.ModelProxyAuditStarted},
		mcpgateway.ModelProxyAuditRecord{AuditID: "stale", UserID: "u1", Status: mcpgateway.ModelProxyAuditStarted, CreatedAt: now.Add(-18 * time.Minute)},
		mcpgateway.ModelProxyAuditRecord{AuditID: "excluded-end", UserID: "u1", CreatedAt: end},
		mcpgateway.ModelProxyAuditRecord{AuditID: "excluded-scope", UserID: "u1", Status: mcpgateway.ModelProxyAuditRejectedScope},
		mcpgateway.ModelProxyAuditRecord{AuditID: "excluded-models", UserID: "u1", Endpoint: "/v1/models", Method: "GET"},
	)
	for i := range records {
		r := &records[i]
		if r.Status == "" {
			r.Status = mcpgateway.ModelProxyAuditSucceeded
		}
		if r.CreatedAt.IsZero() {
			r.CreatedAt = now
		}
		if r.Method == "" {
			r.Method = "POST"
		}
		if r.Endpoint == "" {
			r.Endpoint = "/v1/responses"
		}
		r.ActivityContent = &mcpgateway.ModelActivityContent{SchemaVersion: 1, InputText: "已脱敏文本", InputStatus: "available", OutputStatus: "pending"}
		if r.Status != mcpgateway.ModelProxyAuditStarted {
			r.DurationMS = 100
			r.CompletedAt = r.CreatedAt.Add(100 * time.Millisecond)
			r.ActivityContent.OutputStatus = "unavailable"
		}
		if err := postgres.CreateModelProxyAuditRecord(ctx, *r); err != nil {
			t.Fatal(err)
		}
		if err := memory.CreateModelProxyAuditRecord(ctx, *r); err != nil {
			t.Fatal(err)
		}
	}
	legacy := records[0]
	legacy.AuditID = "legacy"
	legacy.ActivityContent = nil
	if err := postgres.CreateModelProxyAuditRecord(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	_ = memory.CreateModelProxyAuditRecord(ctx, legacy)
	for _, filter := range []mcpgateway.ModelActivityFilter{{}, {UserID: "u1"}, {UserID: "u1", AgentID: "a1", SessionID: "same", TurnID: "same"}, {UserID: "u1", Unattributed: true}, {AgentID: "a1"}, {UserID: "missing"}} {
		filter.Now, filter.Start, filter.End = now, start, end
		filter.Page, filter.PageSize, filter.Limit = 1, 2, 2
		got, err := postgres.ModelActivityOverview(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		want, err := memory.ModelActivityOverview(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		normalizeActivityStats := func(stats *mcpgateway.ModelActivityStats) {
			if stats.LastRequestAt != nil {
				v := stats.LastRequestAt.UTC()
				stats.LastRequestAt = &v
			}
		}
		normalizeActivityStats(&got.Summary.ModelActivityStats)
		for i := range got.Agents {
			normalizeActivityStats(&got.Agents[i].ModelActivityStats)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("summary differs: got=%+v want=%+v", got, want)
		}
		if filter.UserID == "" && filter.AgentID == "" {
			if got.Summary.RequestCount != 8 || got.Summary.TurnCount != 3 || got.Summary.SessionCount != 2 || got.Summary.ActiveUsers != 2 || got.Summary.ActiveAgents != 2 || *got.Summary.TotalTokens != 35 || *got.Summary.SuccessRate != 5.0/6.0 || got.Summary.ProcessingCount != 1 || got.Summary.StaleStartedCount != 1 {
				t.Fatalf("统计口径错误: %+v", got.Summary)
			}
		}
		var ids []string
		for {
			p, err := postgres.ListModelActivityRecords(ctx, filter)
			if err != nil {
				t.Fatal(err)
			}
			m, err := memory.ListModelActivityRecords(ctx, filter)
			if err != nil {
				t.Fatal(err)
			}
			for i := range p.Items {
				p.Items[i].CreatedAt = p.Items[i].CreatedAt.UTC()
				p.Items[i].CompletedAt = p.Items[i].CompletedAt.UTC()
			}
			p.NextCreatedAt = p.NextCreatedAt.UTC()
			if !reflect.DeepEqual(p, m) {
				t.Fatalf("分页不同: postgres=%+v memory=%+v", p, m)
			}
			for _, r := range p.Items {
				ids = append(ids, r.AuditID)
			}
			if p.NextID == "" {
				break
			}
			filter.CursorCreatedAt, filter.CursorID = p.NextCreatedAt, p.NextID
		}
		unique := map[string]bool{}
		for _, id := range ids {
			if unique[id] {
				t.Fatal("游标重复")
			}
			unique[id] = true
		}
		if int64(len(ids)) != got.Summary.RequestCount {
			t.Fatal("游标遗漏")
		}
	}
	if _, err := postgres.GetModelActivityRecord(ctx, "legacy"); !errors.Is(err, mcpgateway.ErrModelProxyAuditNotFound) {
		t.Fatal("历史记录进入P2")
	}
	content := &mcpgateway.ModelActivityContent{SchemaVersion: 1, InputStatus: "unavailable", OutputStatus: "partial", OutputText: "部分输出"}
	update := mcpgateway.ModelProxyAuditFinalize{AuditID: "started", Status: mcpgateway.ModelProxyAuditClientDisconnected, ActivityContent: content, CompletedAt: now, DurationMS: 200}
	if ok, err := postgres.FinalizeModelProxyAuditRecord(ctx, update); err != nil || !ok {
		t.Fatalf("终态写入失败: %v", err)
	}
	record, err := postgres.GetModelActivityRecord(ctx, "started")
	if err != nil || !reflect.DeepEqual(record.ActivityContent, content) {
		t.Fatalf("内容未保存: %+v %v", record, err)
	}
	// 代表性默认页检查索引可用性；不强制优化器为小样本选择索引。
	if _, err := pool.Exec(ctx, `INSERT INTO model_proxy_audit_records (audit_id,user_id,method,endpoint,status,created_at,activity_content)
 SELECT 'bulk_' || n,'u1','POST','/v1/responses','succeeded',now() - n * interval '1 minute','{"schema_version":1}'::jsonb FROM generate_series(1,10000) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE model_proxy_audit_records`); err != nil {
		t.Fatal(err)
	}
	where, args := modelActivityWhere(mcpgateway.ModelActivityFilter{Start: now.AddDate(0, 0, -7), End: end})
	for _, query := range []struct {
		name, sql string
		args      []any
	}{
		{"默认请求页", modelProxyAuditSelectSQL() + ` WHERE ` + where + ` ORDER BY created_at DESC,audit_id DESC LIMIT 20`, args},
		{"默认总览汇总", `SELECT ` + modelActivityAggregateSQL + `, COUNT(DISTINCT user_id), COUNT(DISTINCT (user_id,agent_id)) FILTER (WHERE agent_id <> ''), COUNT(DISTINCT (user_id,agent_id)) FROM model_proxy_audit_records WHERE ` + where, append(append([]any{}, args...), now)},
		{"默认智能体页", `SELECT user_id,agent_id,` + modelActivityAggregateSQL + ` FROM model_proxy_audit_records WHERE ` + where + ` GROUP BY user_id,agent_id ORDER BY MAX(created_at) DESC,user_id,agent_id LIMIT 20`, append(append([]any{}, args...), now)},
	} {
		rows, err := pool.Query(ctx, `EXPLAIN (ANALYZE, BUFFERS) `+query.sql, query.args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			plan.WriteString(line + "\n")
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s:\n%s", query.name, plan.String())
	}
}
