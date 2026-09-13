package codex

import "testing"

func TestNewHandlerAppliesDefaultConfigValues(t *testing.T) {
	h := NewHandler(Config{})

	if h.cfg.Upstream != "https://api.openai.com" {
		t.Fatalf("Upstream = %q", h.cfg.Upstream)
	}
	if h.cfg.ChatGPTUpstream != "https://chatgpt.com/backend-api/codex" {
		t.Fatalf("ChatGPTUpstream = %q", h.cfg.ChatGPTUpstream)
	}
	if h.cfg.ShadowDir == "" {
		t.Fatal("ShadowDir should not be empty")
	}
	if h.cfg.EvictTriggerTokens != 200000 {
		t.Fatalf("EvictTriggerTokens = %d", h.cfg.EvictTriggerTokens)
	}
	if h.cfg.EvictTargetTokens != 150000 {
		t.Fatalf("EvictTargetTokens = %d", h.cfg.EvictTargetTokens)
	}
	if h.cfg.KeepRecentItems != 20 {
		t.Fatalf("KeepRecentItems = %d", h.cfg.KeepRecentItems)
	}
	if h.cfg.MaxFuncOutputChars != 700 {
		t.Fatalf("MaxFuncOutputChars = %d", h.cfg.MaxFuncOutputChars)
	}
	if h.cfg.MaxAssistantChars != 500 {
		t.Fatalf("MaxAssistantChars = %d", h.cfg.MaxAssistantChars)
	}
	if h.cfg.CompBatchSize != 40 {
		t.Fatalf("CompBatchSize = %d", h.cfg.CompBatchSize)
	}
	if h.cfg.FlushTokens != 200000 {
		t.Fatalf("FlushTokens = %d", h.cfg.FlushTokens)
	}
	if h.cfg.SummarizeInterval != 50000 {
		t.Fatalf("SummarizeInterval = %d", h.cfg.SummarizeInterval)
	}
	if h.cfg.SummarizeModel != "gpt-5.4" {
		t.Fatalf("SummarizeModel = %q", h.cfg.SummarizeModel)
	}
}
