package mcpgateway

import (
	"context"
	"sort"
	"time"
)

type ModelActivityFilter struct {
	UserID, AgentID, ClientType, SessionID, TurnID string
	Unattributed                                   bool
	Start, End, Now                                time.Time
	Page, PageSize, Limit                          int
	CursorCreatedAt                                time.Time
	CursorID                                       string
}

type ModelActivityStats struct {
	RequestCount          int64      `json:"request_count"`
	SessionCount          int64      `json:"session_count"`
	TurnCount             int64      `json:"turn_count"`
	TotalTokens           *int64     `json:"total_tokens"`
	SuccessRate           *float64   `json:"success_rate"`
	AvgDurationMS         *float64   `json:"avg_duration_ms"`
	LastRequestAt         *time.Time `json:"last_request_at"`
	ProcessingCount       int64      `json:"processing_count"`
	StaleStartedCount     int64      `json:"stale_started_count"`
	UsageCompleteCount    int64      `json:"usage_complete_count"`
	UsagePartialCount     int64      `json:"usage_partial_count"`
	UsageUnavailableCount int64      `json:"usage_unavailable_count"`
}

type ModelActivitySummary struct {
	ModelActivityStats
	ActiveUsers  int64 `json:"active_users"`
	ActiveAgents int64 `json:"active_agents"`
}

type ModelActivityAgent struct {
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name"`
	ClientType string `json:"client_type"`
	AgentID    string `json:"agent_id"`
	AgentName  string `json:"agent_name"`
	ModelActivityStats
}

type ModelActivityOverview struct {
	Summary ModelActivitySummary `json:"summary"`
	Agents  []ModelActivityAgent `json:"agents"`
	Total   int64                `json:"-"`
}

type ModelActivityStore interface {
	ModelActivityOverview(context.Context, ModelActivityFilter) (ModelActivityOverview, error)
	ListModelActivityRecords(context.Context, ModelActivityFilter) (ModelProxyAuditPage, error)
	GetModelActivityRecord(context.Context, string) (ModelProxyAuditRecord, error)
}

func IsModelActivity(record ModelProxyAuditRecord) bool {
	return record.ActivityContent != nil && record.Method == "POST" &&
		(record.Endpoint == "/v1/responses" || record.Endpoint == "/v1/chat/completions") &&
		record.Status != ModelProxyAuditRejectedAPIKey && record.Status != ModelProxyAuditRejectedScope && record.Status != ModelProxyAuditRejectedAccount
}

func modelActivityMatches(record ModelProxyAuditRecord, filter ModelActivityFilter) bool {
	return IsModelActivity(record) && (filter.UserID == "" || record.UserID == filter.UserID) &&
		(filter.AgentID == "" || record.AgentID == filter.AgentID) &&
		(filter.ClientType == "" || record.ClientType == filter.ClientType) && (!filter.Unattributed || record.AgentID == "") &&
		(filter.SessionID == "" || record.SessionID == filter.SessionID) && (filter.TurnID == "" || record.TurnID == filter.TurnID) &&
		(filter.Start.IsZero() || !record.CreatedAt.Before(filter.Start)) && (filter.End.IsZero() || record.CreatedAt.Before(filter.End))
}

func activityRecordLess(a, b ModelProxyAuditRecord) bool {
	if a.CreatedAt.Equal(b.CreatedAt) {
		return a.AuditID > b.AuditID
	}
	return a.CreatedAt.After(b.CreatedAt)
}

func (s *MemoryStore) ListModelActivityRecords(ctx context.Context, filter ModelActivityFilter) (ModelProxyAuditPage, error) {
	if err := ctx.Err(); err != nil {
		return ModelProxyAuditPage{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]ModelProxyAuditRecord, 0)
	for _, record := range s.modelAudits {
		if !modelActivityMatches(record, filter) {
			continue
		}
		if !filter.CursorCreatedAt.IsZero() && (record.CreatedAt.After(filter.CursorCreatedAt) || (record.CreatedAt.Equal(filter.CursorCreatedAt) && record.AuditID >= filter.CursorID)) {
			continue
		}
		items = append(items, cloneModelProxyAuditRecord(record))
	}
	sort.Slice(items, func(i, j int) bool { return activityRecordLess(items[i], items[j]) })
	page := ModelProxyAuditPage{}
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
		page.NextCreatedAt = items[len(items)-1].CreatedAt
		page.NextID = items[len(items)-1].AuditID
	}
	page.Items = items
	return page, nil
}

func (s *MemoryStore) GetModelActivityRecord(ctx context.Context, id string) (ModelProxyAuditRecord, error) {
	record, err := s.GetModelProxyAuditRecord(ctx, id)
	if err == nil && !IsModelActivity(record) {
		err = ErrModelProxyAuditNotFound
	}
	return record, err
}

func (s *MemoryStore) ModelActivityOverview(ctx context.Context, filter ModelActivityFilter) (ModelActivityOverview, error) {
	if err := ctx.Err(); err != nil {
		return ModelActivityOverview{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]ModelProxyAuditRecord, 0)
	groups := map[[3]string][]ModelProxyAuditRecord{}
	users := map[string]bool{}
	agents := map[[2]string]bool{}
	for _, record := range s.modelAudits {
		if !modelActivityMatches(record, filter) {
			continue
		}
		records = append(records, record)
		key := [3]string{record.UserID, record.ClientType, record.AgentID}
		groups[key] = append(groups[key], record)
		users[record.UserID] = true
		if record.AgentID != "" {
			agents[[2]string{record.UserID, record.AgentID}] = true
		}
	}
	result := ModelActivityOverview{Summary: ModelActivitySummary{ModelActivityStats: aggregateModelActivity(records, filter.Now), ActiveUsers: int64(len(users)), ActiveAgents: int64(len(agents))}, Agents: make([]ModelActivityAgent, 0), Total: int64(len(groups))}
	for key, records := range groups {
		result.Agents = append(result.Agents, ModelActivityAgent{UserID: key[0], ClientType: key[1], AgentID: key[2], ModelActivityStats: aggregateModelActivity(records, filter.Now)})
	}
	sort.Slice(result.Agents, func(i, j int) bool {
		a, b := result.Agents[i], result.Agents[j]
		if a.LastRequestAt.Equal(*b.LastRequestAt) {
			if a.UserID == b.UserID {
				if a.ClientType != b.ClientType {
					return a.ClientType < b.ClientType
				}
				return a.AgentID < b.AgentID
			}
			return a.UserID < b.UserID
		}
		return a.LastRequestAt.After(*b.LastRequestAt)
	})
	start := (filter.Page - 1) * filter.PageSize
	if start > len(result.Agents) {
		start = len(result.Agents)
	}
	end := start + filter.PageSize
	if end > len(result.Agents) {
		end = len(result.Agents)
	}
	result.Agents = result.Agents[start:end]
	return result, nil
}

func aggregateModelActivity(records []ModelProxyAuditRecord, now time.Time) ModelActivityStats {
	stats := ModelActivityStats{}
	sessions := map[[4]string]bool{}
	turns := map[[5]string]bool{}
	var ended, succeeded, duration, tokens int64
	for _, r := range records {
		stats.RequestCount++
		if r.SessionID != "" {
			sessions[[4]string{r.UserID, r.ClientType, r.AgentID, r.SessionID}] = true
			if r.TurnID != "" {
				turns[[5]string{r.UserID, r.ClientType, r.AgentID, r.SessionID, r.TurnID}] = true
			}
		}
		if stats.LastRequestAt == nil || r.CreatedAt.After(*stats.LastRequestAt) {
			at := r.CreatedAt
			stats.LastRequestAt = &at
		}
		if r.Status == ModelProxyAuditStarted {
			if !r.CreatedAt.Before(now.Add(-17 * time.Minute)) {
				stats.ProcessingCount++
			} else {
				stats.StaleStartedCount++
			}
		} else {
			ended++
			duration += r.DurationMS
			if r.Status == ModelProxyAuditSucceeded {
				succeeded++
			}
		}
		switch ModelProxyAuditUsageStatus(r) {
		case ModelProxyUsageComplete:
			stats.UsageCompleteCount++
			tokens += r.TotalTokens
		case ModelProxyUsagePartial:
			stats.UsagePartialCount++
			tokens += r.TotalTokens
		default:
			stats.UsageUnavailableCount++
		}
	}
	stats.SessionCount = int64(len(sessions))
	stats.TurnCount = int64(len(turns))
	if stats.UsageCompleteCount+stats.UsagePartialCount > 0 {
		stats.TotalTokens = &tokens
	}
	if ended > 0 {
		rate := float64(succeeded) / float64(ended)
		avg := float64(duration) / float64(ended)
		stats.SuccessRate = &rate
		stats.AvgDurationMS = &avg
	}
	return stats
}
