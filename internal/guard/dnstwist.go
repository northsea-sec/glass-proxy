package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

type dnstwistEntry struct {
	Fuzzer     string `json:"fuzzer"`
	Domain     string `json:"domain"`
	DNSA       string `json:"dns-a,omitempty"`
	DNSAAAA    string `json:"dns-aaaa,omitempty"`
	DNSMX      string `json:"dns-mx,omitempty"`
	DNSNS      string `json:"dns-ns,omitempty"`
	GeoCountry string `json:"geoip-country,omitempty"`
}

// checkDnstwist runs dnstwist against the given host and checks if it is a
// lookalike of any protected domain. Returns true if the host appears to be
// a typosquatting/homograph impersonation of a protected domain.
func (s *Server) checkDnstwist(ctx context.Context, host string) (bool, string) {
	if len(s.cfg.ProtectedDomains) == 0 {
		return false, ""
	}

	// Check if the host is itself a protected domain (exact match = safe)
	hostLower := strings.ToLower(strings.TrimSuffix(host, "."))
	for _, pd := range s.cfg.ProtectedDomains {
		if strings.ToLower(pd) == hostLower {
			return false, ""
		}
	}

	// For each protected domain, run dnstwist and check if the host appears
	// in the permutation results.
	for _, protected := range s.cfg.ProtectedDomains {
		if s.isDnstwistMatch(ctx, protected, hostLower) {
			return true, fmt.Sprintf("domain %q looks like typosquatting of %q (dnstwist)", host, protected)
		}
	}

	return false, ""
}

func (s *Server) isDnstwistMatch(ctx context.Context, protectedDomain, targetHost string) bool {
	cacheKey := hashKey("dnstwist", protectedDomain)

	// Check cache for precomputed permutations
	if cached, ok := s.urlCache.get(cacheKey); ok {
		// We stash the permutation list in the Reasons field for caching
		for _, perm := range cached.Reasons {
			if perm == targetHost {
				return true
			}
		}
		return false
	}

	bin := s.venvBin("dnstwist")
	out, err := runSubprocess(ctx, 30*time.Second, bin, "--format", "json", "--registered", protectedDomain)
	if err != nil {
		if len(out) == 0 {
			log.Printf("[GUARD-DNSTWIST] failed for %s: %v", protectedDomain, err)
			return false
		}
	}

	var entries []dnstwistEntry
	if jsonErr := json.Unmarshal(out, &entries); jsonErr != nil {
		log.Printf("[GUARD-DNSTWIST] JSON parse error for %s: %v", protectedDomain, jsonErr)
		return false
	}

	// Collect all permutation domains into a set
	permDomains := make([]string, 0, len(entries))
	found := false
	for _, entry := range entries {
		d := strings.ToLower(strings.TrimSuffix(entry.Domain, "."))
		if d == "" || entry.Fuzzer == "*original" {
			continue
		}
		permDomains = append(permDomains, d)
		if d == targetHost {
			found = true
		}
	}

	// Cache the permutation list (reusing URLCheckResponse.Reasons as storage)
	s.urlCache.set(cacheKey, URLCheckResponse{
		Allowed: true,
		Reasons: permDomains,
	})

	return found
}
