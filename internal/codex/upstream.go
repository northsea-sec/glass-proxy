package codex

import (
	"crypto/tls"
	"net/http"
	"strings"
)

const defaultOpenAIUpstream = "https://api.openai.com"
const defaultChatGPTCodexUpstream = "https://chatgpt.com/backend-api/codex"

func normalizeOpenAIUpstream(upstream string) string {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		upstream = defaultOpenAIUpstream
	}
	return strings.TrimRight(upstream, "/")
}

func normalizeChatGPTUpstream(upstream string) string {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		upstream = defaultChatGPTCodexUpstream
	}
	return strings.TrimRight(upstream, "/")
}

func bearerToken(authHeader string) string {
	authHeader = strings.TrimSpace(authHeader)
	if len(authHeader) < len("Bearer ")+1 || !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return ""
	}
	return strings.TrimSpace(authHeader[len("Bearer "):])
}

func isLikelyOpenAIAPIKey(token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	return strings.HasPrefix(token, "sk-")
}

func looksLikeJWT(token string) bool {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
				return false
			}
		}
	}
	return true
}

func shouldUseChatGPTUpstream(authHeader string) bool {
	token := bearerToken(authHeader)
	if token == "" || isLikelyOpenAIAPIKey(token) {
		return false
	}
	return looksLikeJWT(token)
}

func authRoutingMode(authHeader string) string {
	token := bearerToken(authHeader)
	switch {
	case token == "":
		return "none"
	case isLikelyOpenAIAPIKey(token):
		return "api_key"
	case looksLikeJWT(token):
		return "chatgpt_jwt"
	default:
		return "bearer_other"
	}
}

func resolveResponsesUpstream(openAIUpstream, chatgptUpstream, authHeader string) string {
	if shouldUseChatGPTUpstream(authHeader) {
		return normalizeChatGPTUpstream(chatgptUpstream) + "/responses"
	}
	return normalizeOpenAIUpstream(openAIUpstream) + "/v1/responses"
}

func resolveCodexUpstreamBase(openAIUpstream, chatgptUpstream, authHeader string) string {
	if shouldUseChatGPTUpstream(authHeader) {
		return normalizeChatGPTUpstream(chatgptUpstream)
	}
	return normalizeOpenAIUpstream(openAIUpstream) + "/v1"
}

func newChatGPTUpstreamTransport() http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	return transport
}
