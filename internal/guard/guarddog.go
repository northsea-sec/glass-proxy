package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

func guardDogEcosystem(manager string) string {
	switch strings.ToLower(manager) {
	case "npm":
		return "npm"
	case "pip", "pypi", "python":
		return "pypi"
	default:
		return ""
	}
}

type guardDogResult struct {
	Issues []guardDogIssue `json:"issues"`
	Errors []string        `json:"errors"`
}

type guardDogIssue struct {
	Code     string `json:"code"`
	Location string `json:"location"`
	Message  string `json:"message"`
}

func (s *Server) runGuardDog(ctx context.Context, manager, name string) (bool, []string) {
	eco := guardDogEcosystem(manager)
	if eco == "" {
		return false, nil
	}

	bin := s.venvBin("guarddog")
	out, err := runSubprocess(ctx, 60*time.Second, bin, eco, "scan", name, "--output-format", "json")
	if err != nil {
		// guarddog exits non-zero when it finds issues; partial stdout may still have JSON
		if len(out) == 0 {
			log.Printf("[GUARD-GUARDDOG] %s scan %s failed: %v", eco, name, err)
			return false, nil
		}
	}

	var result map[string]interface{}
	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		log.Printf("[GUARD-GUARDDOG] JSON parse error for %s/%s: %v", eco, name, jsonErr)
		return false, nil
	}

	var reasons []string
	// GuardDog output is { "package_name": { "results": { "rule_name": { ... } } } }
	for _, pkgData := range result {
		pkgMap, ok := pkgData.(map[string]interface{})
		if !ok {
			continue
		}
		results, ok := pkgMap["results"].(map[string]interface{})
		if !ok {
			continue
		}
		for ruleName, ruleData := range results {
			ruleMap, ok := ruleData.(map[string]interface{})
			if !ok {
				reasons = append(reasons, fmt.Sprintf("GuardDog: %s flagged", ruleName))
				continue
			}
			if locations, ok := ruleMap["locations"].([]interface{}); ok && len(locations) > 0 {
				reasons = append(reasons, fmt.Sprintf("GuardDog: %s (%d locations)", ruleName, len(locations)))
			} else {
				reasons = append(reasons, fmt.Sprintf("GuardDog: %s flagged", ruleName))
			}
		}
	}

	return len(reasons) > 0, reasons
}
