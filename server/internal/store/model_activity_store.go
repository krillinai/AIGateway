package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/krillinai/Clawee/server/internal/mcpgateway"
)

const modelActivityCondition = `activity_content IS NOT NULL AND method = 'POST'
 AND endpoint IN ('/v1/responses', '/v1/chat/completions')
 AND status NOT IN ('rejected_api_key', 'rejected_scope', 'rejected_account')`

func modelActivityWhere(f mcpgateway.ModelActivityFilter) (string, []any) {
	clauses := []string{modelActivityCondition, "created_at >= $1", "created_at < $2"}
	args := []any{f.Start, f.End}
	for _, field := range []struct{ name, value string }{{"user_id", f.UserID}, {"agent_id", f.AgentID}, {"client_type", f.ClientType}, {"session_id", f.SessionID}, {"turn_id", f.TurnID}} {
		if field.value != "" {
			args = append(args, field.value)
			clauses = append(clauses, fmt.Sprintf("%s = $%d", field.name, len(args)))
		}
	}
	if f.Unattributed {
		clauses = append(clauses, "agent_id = ''")
	}
	return strings.Join(clauses, " AND "), args
}

const modelActivityAggregateSQL = `COUNT(*),
 COUNT(DISTINCT (user_id,client_type,agent_id,session_id)) FILTER (WHERE session_id <> ''),
 COUNT(DISTINCT (user_id,client_type,agent_id,session_id,turn_id)) FILTER (WHERE session_id <> '' AND turn_id <> ''),
 SUM(total_tokens) FILTER (WHERE usage_status IN ('complete','partial')),
 COUNT(*) FILTER (WHERE status = 'succeeded')::float8 / NULLIF(COUNT(*) FILTER (WHERE status <> 'started'),0),
 AVG(duration_ms) FILTER (WHERE status <> 'started'), MAX(created_at),
 COUNT(*) FILTER (WHERE status = 'started' AND created_at >= $3::timestamptz - INTERVAL '17 minutes'),
 COUNT(*) FILTER (WHERE status = 'started' AND created_at < $3::timestamptz - INTERVAL '17 minutes'),
 COUNT(*) FILTER (WHERE usage_status = 'complete'), COUNT(*) FILTER (WHERE usage_status = 'partial'),
 COUNT(*) FILTER (WHERE usage_status NOT IN ('complete','partial'))`

func modelActivityScanStats(stats *mcpgateway.ModelActivityStats) []any {
	return []any{&stats.RequestCount, &stats.SessionCount, &stats.TurnCount, &stats.TotalTokens, &stats.SuccessRate, &stats.AvgDurationMS, &stats.LastRequestAt, &stats.ProcessingCount, &stats.StaleStartedCount, &stats.UsageCompleteCount, &stats.UsagePartialCount, &stats.UsageUnavailableCount}
}

func (s *MCPGatewayStore) ModelActivityOverview(ctx context.Context, f mcpgateway.ModelActivityFilter) (mcpgateway.ModelActivityOverview, error) {
	result := mcpgateway.ModelActivityOverview{Agents: make([]mcpgateway.ModelActivityAgent, 0)}
	if s == nil || s.pool == nil {
		return result, mcpgateway.ErrModelProxyAuditUnavailable
	}
	// $3 固定为统计时刻，筛选参数从 $4 开始。
	where, args := modelActivityWhere(f)
	for i := len(args); i >= 3; i-- {
		where = strings.ReplaceAll(where, fmt.Sprintf("$%d", i), fmt.Sprintf("$%d", i+1))
	}
	args = append(args[:2], append([]any{f.Now}, args[2:]...)...)
	scan := modelActivityScanStats(&result.Summary.ModelActivityStats)
	scan = append(scan, &result.Summary.ActiveUsers, &result.Summary.ActiveAgents, &result.Total)
	err := s.pool.QueryRow(ctx, `SELECT `+modelActivityAggregateSQL+`, COUNT(DISTINCT user_id),
 COUNT(DISTINCT (user_id,agent_id)) FILTER (WHERE agent_id <> ''), COUNT(DISTINCT (user_id,client_type,agent_id))
 FROM model_proxy_audit_records WHERE `+where, args...).Scan(scan...)
	if err != nil {
		return result, err
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := s.pool.Query(ctx, `SELECT user_id,client_type,agent_id,`+modelActivityAggregateSQL+` FROM model_proxy_audit_records WHERE `+where+`
 GROUP BY user_id,client_type,agent_id ORDER BY MAX(created_at) DESC,user_id,client_type,agent_id`+fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var agent mcpgateway.ModelActivityAgent
		scan := append([]any{&agent.UserID, &agent.ClientType, &agent.AgentID}, modelActivityScanStats(&agent.ModelActivityStats)...)
		if err := rows.Scan(scan...); err != nil {
			return result, err
		}
		result.Agents = append(result.Agents, agent)
	}
	return result, rows.Err()
}

func (s *MCPGatewayStore) ListModelActivityRecords(ctx context.Context, f mcpgateway.ModelActivityFilter) (mcpgateway.ModelProxyAuditPage, error) {
	result := mcpgateway.ModelProxyAuditPage{Items: make([]mcpgateway.ModelProxyAuditRecord, 0)}
	if s == nil || s.pool == nil {
		return result, mcpgateway.ErrModelProxyAuditUnavailable
	}
	where, args := modelActivityWhere(f)
	if !f.CursorCreatedAt.IsZero() {
		args = append(args, f.CursorCreatedAt, f.CursorID)
		where += fmt.Sprintf(" AND (created_at,audit_id) < ($%d,$%d)", len(args)-1, len(args))
	}
	args = append(args, f.Limit+1)
	rows, err := s.pool.Query(ctx, modelProxyAuditSelectSQL()+` WHERE `+where+fmt.Sprintf(" ORDER BY created_at DESC,audit_id DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanModelProxyAuditRecord(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, record)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > f.Limit {
		result.Items = result.Items[:f.Limit]
		last := result.Items[f.Limit-1]
		result.NextCreatedAt = last.CreatedAt
		result.NextID = last.AuditID
	}
	return result, nil
}

func (s *MCPGatewayStore) GetModelActivityRecord(ctx context.Context, id string) (mcpgateway.ModelProxyAuditRecord, error) {
	if s == nil || s.pool == nil {
		return mcpgateway.ModelProxyAuditRecord{}, mcpgateway.ErrModelProxyAuditUnavailable
	}
	record, err := scanModelProxyAuditRecord(s.pool.QueryRow(ctx, modelProxyAuditSelectSQL()+` WHERE audit_id = $1 AND `+modelActivityCondition, id))
	if errors.Is(err, pgx.ErrNoRows) {
		err = mcpgateway.ErrModelProxyAuditNotFound
	}
	return record, err
}
