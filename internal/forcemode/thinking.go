// Package forcemode injects/overrides thinking parameters in API requests.
// Ported from mitm_itt_addon.py FORCE_THINKING_MODE logic.
//
// Environment variables:
//
//	FORCE_THINKING_MODE=1       - Force thinking enabled on all requests
//	FORCE_THINKING_BUDGET=31999 - Force specific thinking budget tokens
//	FORCE_THINKING_BUDGET=0     - Disable thinking entirely
//	FORCE_INTERLEAVED=1         - Enable interleaved thinking (200k context)
//	FORCE_INTERLEAVED=0         - Disable interleaved thinking
//	BLOCK_NON_OPUS=1            - Reject requests for non-opus models with 403
package forcemode

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// Config holds force mode settings, loaded from env once at startup.
type Config struct {
	ForceThinking    bool
	ThinkingBudget   int   // 0 = disable thinking, >0 = forced budget
	ForceInterleaved *bool // nil = don't touch, true/false = force
	BlockNonOpus     bool
}

// LoadFromEnv reads force mode config from environment variables.
func LoadFromEnv() Config {
	cfg := Config{}

	if v := os.Getenv("FORCE_THINKING_MODE"); v == "1" {
		cfg.ForceThinking = true
	}

	if v := os.Getenv("FORCE_THINKING_BUDGET"); v != "" {
		if budget, err := strconv.Atoi(v); err == nil {
			cfg.ThinkingBudget = budget
		}
	}

	if v := os.Getenv("FORCE_INTERLEAVED"); v != "" {
		val := v == "1"
		cfg.ForceInterleaved = &val
	}

	if v := os.Getenv("BLOCK_NON_OPUS"); v == "1" {
		cfg.BlockNonOpus = true
	}

	return cfg
}

// CheckModel returns an error if the model is blocked by BLOCK_NON_OPUS.
func (c Config) CheckModel(model string) error {
	if !c.BlockNonOpus {
		return nil
	}
	model = strings.ToLower(model)
	if strings.Contains(model, "opus") {
		return nil
	}
	return fmt.Errorf("model %q blocked: BLOCK_NON_OPUS=1, only opus models allowed", model)
}

// Apply modifies the API request body to enforce thinking parameters.
// Returns true if any modifications were made.
//
// Effort-aware: if the request carries an "effort" field (Claude Code .68+),
// we respect the client's effort level instead of blindly forcing max budget.
//   - effort "high"   -> use our forced budget (unchanged behavior)
//   - effort "medium" -> cap budget at min(10000, forced_budget)
//   - effort "low"    -> skip thinking forcing entirely
func (c Config) Apply(body map[string]interface{}) bool {
	if !c.ForceThinking && c.ThinkingBudget == 0 && c.ForceInterleaved == nil {
		return false
	}

	modified := false

	// Determine effective budget based on client effort level
	effectiveBudget := c.ThinkingBudget
	if effort, ok := body["effort"].(string); ok {
		switch strings.ToLower(effort) {
		case "low":
			log.Printf("[FORCE] Respecting effort=low, skipping thinking override")
			return false
		case "medium":
			if effectiveBudget > 10000 {
				effectiveBudget = 10000
			}
			log.Printf("[FORCE] Respecting effort=medium, capping budget at %d", effectiveBudget)
		default:
			// "high" or unrecognized: use full forced budget
		}
	}

	// Force thinking on
	if c.ForceThinking {
		thinking, _ := body["thinking"].(map[string]interface{})
		if thinking == nil {
			thinking = make(map[string]interface{})
			body["thinking"] = thinking
		}

		// Enable thinking
		if thinking["type"] != "enabled" {
			thinking["type"] = "enabled"
			modified = true
			log.Printf("[FORCE] Enabled thinking")
		}

		// Set budget
		if effectiveBudget > 0 {
			currentBudget, _ := requestInt(thinking["budget_tokens"])
			if currentBudget != effectiveBudget {
				thinking["budget_tokens"] = effectiveBudget
				modified = true
				log.Printf("[FORCE] Set thinking budget: %d", effectiveBudget)
			}
			if maxTokens, ok := requestInt(body["max_tokens"]); ok && maxTokens <= effectiveBudget {
				body["max_tokens"] = effectiveBudget + 1
				modified = true
				log.Printf("[FORCE] Raised max_tokens to %d to preserve thinking budget %d",
					effectiveBudget+1, effectiveBudget)
			}
		}

		// Safety net: API requires budget_tokens when type=enabled.
		// If we enabled thinking but have no budget (misconfiguration),
		// inject a default matching the max usable budget to avoid 400 errors.
		if _, hasBudget := thinking["budget_tokens"]; !hasBudget {
			const defaultBudget = 31999
			thinking["budget_tokens"] = defaultBudget
			modified = true
			log.Printf("[FORCE] WARNING: thinking enabled without budget_tokens — injecting default %d (check force_thinking_budget config)", defaultBudget)
		}
	}

	// Force thinking OFF (budget=0 means disable)
	if c.ThinkingBudget == 0 && !c.ForceThinking {
		if _, hasThinking := body["thinking"]; hasThinking {
			delete(body, "thinking")
			modified = true
			log.Printf("[FORCE] Disabled thinking (budget=0)")
		}
	}

	// Force interleaved thinking
	if c.ForceInterleaved != nil {
		thinking, _ := body["thinking"].(map[string]interface{})
		if thinking != nil {
			if *c.ForceInterleaved {
				if thinking["interleaved"] != true {
					thinking["interleaved"] = true
					modified = true
					log.Printf("[FORCE] Enabled interleaved thinking")
				}
			} else {
				if _, has := thinking["interleaved"]; has {
					delete(thinking, "interleaved")
					modified = true
					log.Printf("[FORCE] Disabled interleaved thinking")
				}
			}
		}
	}

	// Final safety: ensure any request with thinking.type=enabled has budget_tokens.
	// Catches client-originated thinking params that lack budget_tokens and keeps
	// max_tokens above the active budget even when the budget came from a
	// fallback or legacy config path.
	if thinking, ok := body["thinking"].(map[string]interface{}); ok {
		if tp, _ := thinking["type"].(string); tp == "enabled" {
			if _, hasBudget := thinking["budget_tokens"]; !hasBudget {
				const fallbackBudget = 31999
				thinking["budget_tokens"] = fallbackBudget
				modified = true
				log.Printf("[FORCE] Safety: client sent thinking.type=enabled without budget_tokens — injecting %d", fallbackBudget)
			}
			if budget, ok := requestInt(thinking["budget_tokens"]); ok {
				if maxTokens, ok := requestInt(body["max_tokens"]); ok && maxTokens <= budget {
					body["max_tokens"] = budget + 1
					modified = true
					log.Printf("[FORCE] Raised max_tokens to %d to preserve thinking budget %d",
						budget+1, budget)
				}
			}
		}
	}

	return modified
}

func requestInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		i := int(n)
		return i, float64(i) == n
	default:
		return 0, false
	}
}
