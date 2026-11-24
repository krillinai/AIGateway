package server

import (
	"context"
	"errors"
	"time"

	"github.com/krillinai/Clawee/server/internal/accounts"
	"github.com/krillinai/Clawee/server/internal/mcpgateway"
)

func isAccountTokenAgentID(agentID string) bool {
	clientType, _, ok := mcpgateway.ParseExternalAgentID(agentID)
	return ok && clientType == mcpgateway.ExternalAgentClientAccount
}

func materializeExternalAgentsForUser(ctx context.Context, opts Options, userID string) {
	if modelAccessMode(opts) != ModelAccessPlatformManaged || opts.ProxyGateway == nil || opts.AccountService == nil {
		return
	}
	activityStore, ok := opts.ProxyGateway.Store().(mcpgateway.ModelActivityStore)
	if !ok {
		return
	}
	now := time.Now().UTC()
	for _, clientType := range []string{
		mcpgateway.ModelActivityClientCodex,
		mcpgateway.ModelActivityClientWorkBuddy,
		mcpgateway.ModelActivityClientClaude,
		mcpgateway.ModelActivityClientGeneric,
	} {
		overview, err := activityStore.ModelActivityOverview(ctx, mcpgateway.ModelActivityFilter{
			UserID: userID, ClientType: clientType, Start: now.AddDate(0, 0, -30), End: now.Add(time.Second), Now: now,
			Page: 1, PageSize: 1000,
		})
		if err != nil {
			continue
		}
		for _, activity := range overview.Agents {
			if activity.UserID != userID || activity.AgentID != "" || activity.LastRequestAt == nil {
				continue
			}
			_ = ensureExternalAgent(ctx, opts, userID, clientType, activity.LastRequestAt.UTC())
		}
	}
}

func ensureExternalAgent(ctx context.Context, opts Options, userID, clientType string, createdAt time.Time) error {
	agentID := mcpgateway.ExternalAgentID(clientType, userID)
	if agentID == "" || opts.AccountService == nil || opts.ProxyGateway == nil {
		return accounts.ErrInvalidAccountRequest
	}
	if owner, err := opts.AccountService.AccountForAgent(ctx, agentID); err == nil {
		if owner.UserID != userID {
			return mcpgateway.ErrAgentOwnerUnavailable
		}
		agent, agentErr := opts.ProxyGateway.Store().GetAgent(ctx, agentID)
		if agentErr != nil {
			return agentErr
		}
		if agent.Status != mcpgateway.StatusActive {
			return mcpgateway.ErrAgentOwnerUnavailable
		}
		return nil
	} else if !errors.Is(err, accounts.ErrAccountAgentNotFound) {
		return err
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	err := opts.ProxyGateway.CreateOwnedAgent(ctx, userID, mcpgateway.AgentRegistration{
		AgentID: agentID, ClientID: clientType, Name: modelActivityClientLabel(clientType), ActorID: userID,
		Status: mcpgateway.StatusActive, CreationSource: mcpgateway.AgentCreationSourceExternal, CreatedAt: createdAt,
	})
	if err != nil && !errors.Is(err, mcpgateway.ErrAgentAlreadyExists) {
		return err
	}
	return opts.AccountService.BindAgent(ctx, userID, agentID)
}
