package guard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const osvAPIURL = "https://api.osv.dev/v1/query"

type osvQueryRequest struct {
	Package *osvPackage `json:"package,omitempty"`
	Version string      `json:"version,omitempty"`
}

type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvQueryResponse struct {
	Vulns []osvVuln `json:"vulns"`
}

type osvVuln struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

func osvEcosystem(manager string) string {
	switch strings.ToLower(manager) {
	case "npm":
		return "npm"
	case "pip", "pypi", "python":
		return "PyPI"
	case "go", "golang":
		return "Go"
	case "maven", "java":
		return "Maven"
	case "cargo", "rust":
		return "crates.io"
	case "nuget", "dotnet":
		return "NuGet"
	case "rubygems", "gem", "ruby":
		return "RubyGems"
	default:
		return ""
	}
}

func (s *Server) lookupOSVMalicious(ctx context.Context, manager, name, version string) (bool, []string) {
	ecosystem := osvEcosystem(manager)
	if ecosystem == "" {
		return false, nil
	}

	reqBody := osvQueryRequest{
		Package: &osvPackage{
			Name:      name,
			Ecosystem: ecosystem,
		},
	}
	if version != "" {
		reqBody.Version = version
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("[GUARD-OSV] marshal error: %v", err)
		return false, nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "POST", osvAPIURL, bytes.NewReader(data))
	if err != nil {
		log.Printf("[GUARD-OSV] request error: %v", err)
		return false, nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "glass-guardd/1.0")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[GUARD-OSV] HTTP error for %s/%s: %v", ecosystem, name, err)
		return false, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		log.Printf("[GUARD-OSV] HTTP %d for %s/%s", resp.StatusCode, ecosystem, name)
		return false, nil
	}

	var osvResp osvQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&osvResp); err != nil {
		log.Printf("[GUARD-OSV] decode error: %v", err)
		return false, nil
	}

	var reasons []string
	for _, vuln := range osvResp.Vulns {
		if strings.HasPrefix(vuln.ID, "MAL-") {
			summary := vuln.Summary
			if summary == "" {
				summary = "known malicious package"
			}
			reasons = append(reasons, fmt.Sprintf("OSV %s: %s", vuln.ID, summary))
		}
	}
	return len(reasons) > 0, reasons
}
