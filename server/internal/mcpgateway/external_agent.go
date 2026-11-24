package mcpgateway

import "strings"

const ExternalAgentClientAccount = "account_token"

// ExternalAgentID returns the stable Agent ID used for a model client that is
// discovered from model activity rather than an installed internal Agent.
func ExternalAgentID(clientType, userID string) string {
	if !IsExternalAgentClient(clientType) || strings.TrimSpace(userID) == "" {
		return ""
	}
	return "external:" + clientType + ":" + strings.TrimSpace(userID)
}

func ParseExternalAgentID(agentID string) (clientType, userID string, ok bool) {
	parts := strings.Split(strings.TrimSpace(agentID), ":")
	if len(parts) != 3 || parts[0] != "external" || !IsExternalAgentClient(parts[1]) || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func IsExternalAgentClient(clientType string) bool {
	switch strings.TrimSpace(clientType) {
	case ModelActivityClientCodex, ModelActivityClientClaude, ModelActivityClientWorkBuddy, ModelActivityClientGeneric, ExternalAgentClientAccount:
		return true
	default:
		return false
	}
}
