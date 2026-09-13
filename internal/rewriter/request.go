// request.go — request body rewriting.
// JSON body rewriting for API mode (modify messages, tools, system prompt).
// Passthrough for web mode (HTML forms, etc).
package rewriter

import (
	"bytes"
	"encoding/json"
)

// RewriteRequestBody rewrites domain references in a JSON API request body.
// Used for CLI API mode where the body contains JSON with potential domain refs.
func RewriteRequestBody(body []byte, filters []SubFilter) []byte {
	if len(body) == 0 || len(filters) == 0 {
		return body
	}

	// Quick check: does the body even contain any of our find strings?
	needsRewrite := false
	for _, f := range filters {
		if bytes.Contains(body, []byte(f.Find)) {
			needsRewrite = true
			break
		}
	}
	if !needsRewrite {
		return body
	}

	// For JSON bodies: parse, rewrite string values, re-serialize
	// This is safer than blind byte replacement which could break JSON structure
	var parsed interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		// Not valid JSON — do simple byte replacement
		return RewriteBody(body, "application/json", filters)
	}

	rewriteValue(parsed, filters)

	result, err := json.Marshal(parsed)
	if err != nil {
		return body
	}
	return result
}

// rewriteValue recursively rewrites string values in a JSON structure.
func rewriteValue(v interface{}, filters []SubFilter) {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			if s, ok := child.(string); ok {
				for _, f := range filters {
					s = bytes.NewBuffer(bytes.ReplaceAll([]byte(s), []byte(f.Find), []byte(f.Replace))).String()
				}
				val[k] = s
			} else {
				rewriteValue(child, filters)
			}
		}
	case []interface{}:
		for i, child := range val {
			if s, ok := child.(string); ok {
				for _, f := range filters {
					s = bytes.NewBuffer(bytes.ReplaceAll([]byte(s), []byte(f.Find), []byte(f.Replace))).String()
				}
				val[i] = s
			} else {
				rewriteValue(child, filters)
			}
		}
	}
}
