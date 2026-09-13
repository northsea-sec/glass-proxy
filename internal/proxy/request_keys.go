package proxy

import (
	"context"

	"proxy.local/app/internal/glass"
	"proxy.local/app/internal/subagent"
	"proxy.local/app/internal/trimmer"
)

func deriveRequestKey(requestSessionKey, affinityKey string, info subagent.Classification, body map[string]interface{}) string {
	key := requestSessionKey
	if key == "" {
		key = affinityKey
	}
	if !info.BypassMessageCache {
		return key
	}
	if suffix := subagent.StableSessionSuffix(subagent.SystemBlocks(body)); suffix != "" {
		return requestSessionKey + suffix
	}
	if key == affinityKey && key != "" {
		return key + "_child"
	}
	return key
}

func effectiveRequestConvID(gr glass.ProcessResult) string {
	if gr.RequestKey != "" {
		return gr.RequestKey
	}
	if gr.SessionKey != "" {
		return gr.SessionKey
	}
	return gr.AffinityKey
}

func effectiveStreamingConvID(ctx context.Context, reqBody map[string]interface{}) string {
	if convID := convIDFrom(ctx); convID != "" {
		return convID
	}
	if gr, ok := glassResultFrom(ctx).(glass.ProcessResult); ok {
		if convID := effectiveRequestConvID(gr); convID != "" {
			return convID
		}
	}
	return trimmer.SessionFingerprint(reqBody, 0)
}

func effectiveStreamingSubagent(reqBody map[string]interface{}, ctx context.Context) subagent.Classification {
	if gr, ok := glassResultFrom(ctx).(glass.ProcessResult); ok {
		if gr.Subagent.IsSubagent || gr.Subagent.Type != "" {
			return gr.Subagent
		}
	}
	return subagent.Classify(reqBody)
}

func shouldUpdateGlassTokens(gr glass.ProcessResult) bool {
	return !gr.Subagent.BypassMessageCache
}
