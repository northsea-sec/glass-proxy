package glass

import "proxy.local/app/internal/subagent"

// RequestMeta carries request-scoped metadata that should not be smuggled
// through the JSON request body.
type RequestMeta struct {
	SessionKey       string
	RequestKey       string
	AffinityKey      string
	ClientPID        int
	APIKey           string
	AuthIsBearer     bool // true when APIKey came from Authorization: Bearer (not x-api-key)
	AnthropicVersion string
	Betas            []string
	Subagent         subagent.Classification
}
