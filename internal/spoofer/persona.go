// Package spoofer — persona.go generates deterministic browser fingerprints
// per tenant per day. The persona is derived from HMAC(key, tenant+date) so:
//   - Same tenant, same day   → same persona (consistent, not suspicious)
//   - Same tenant, new day    → new persona (Chrome auto-updated, natural)
//   - Different tenant         → different persona (different user, expected)
//
// The generated persona includes: User-Agent, Accept-Language, Sec-Ch-Ua,
// Sec-Ch-Ua-Platform, Sec-Ch-Ua-Mobile. All values are drawn from pools of
// real-world browser strings observed in the wild.
package spoofer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// PersonaManager generates and caches browser personas.
type PersonaManager struct {
	key   []byte // HMAC key for deterministic generation
	mu    sync.RWMutex
	cache map[string]*Persona // tenant+date → persona
}

// Persona is a complete browser identity.
type Persona struct {
	UserAgent       string
	AcceptLanguage  string
	SecChUa         string
	SecChUaPlatform string
	SecChUaMobile   string
	Date            string // YYYY-MM-DD this persona is valid for
}

// Chrome major versions 120-135 (Dec 2023 - Feb 2026 releases).
// Brand strings rotate per version — verified against real Chrome releases.
// G6 fix: Extended from 120-131 to 120-135 to cover current stable channel.
var chromeVersions = []struct {
	full  string // e.g. "131.0.6778.85"
	major int    // e.g. 131
	brand string // "Not_A Brand" varies by version
}{
	{"120.0.6099.109", 120, `"Not_A Brand";v="8"`},
	{"121.0.6167.85", 121, `"Not A(Brand";v="99"`},
	{"122.0.6261.94", 122, `"Chromium";v="122"`},
	{"123.0.6312.86", 123, `"Not/A)Brand";v="8"`},
	{"124.0.6367.91", 124, `"Not-A.Brand";v="99"`},
	{"125.0.6422.76", 125, `"Not/A)Brand";v="8"`},
	{"126.0.6478.114", 126, `"Not/A)Brand";v="8"`},
	{"127.0.6533.72", 127, `"Not)A;Brand";v="99"`},
	{"128.0.6613.84", 128, `"Not;A=Brand";v="8"`},
	{"129.0.6668.58", 129, `"Not/A)Brand";v="8"`},
	{"130.0.6723.69", 130, `"Not?A_Brand";v="8"`},
	{"131.0.6778.85", 131, `"Not_A Brand";v="8"`},
	{"132.0.6834.110", 132, `"Not A(Brand";v="8"`},
	{"133.0.6943.127", 133, `"Not:A-Brand";v="99"`},
	{"134.0.7077.82", 134, `"Not/A)Brand";v="8"`},
	{"135.0.7049.52", 135, `"Not_A Brand";v="8"`},
}

// OS platforms — weighted toward reality (Windows ~65%, macOS ~20%, Linux ~15%).
var osPlatforms = []struct {
	ua         string // User-Agent OS string
	chPlatform string // Sec-Ch-Ua-Platform value
	weight     int
}{
	{"Windows NT 10.0; Win64; x64", `"Windows"`, 50},
	{"Windows NT 11.0; Win64; x64", `"Windows"`, 15},
	{"Macintosh; Intel Mac OS X 10_15_7", `"macOS"`, 12},
	{"Macintosh; Intel Mac OS X 14_0", `"macOS"`, 8},
	{"X11; Linux x86_64", `"Linux"`, 10},
	{"X11; Ubuntu; Linux x86_64", `"Linux"`, 5},
}

// Accept-Language values — weighted toward English-primary.
var acceptLanguages = []string{
	"en-US,en;q=0.9",
	"en-US,en;q=0.9,es;q=0.8",
	"en-GB,en;q=0.9,en-US;q=0.8",
	"en-US,en;q=0.9,fr;q=0.8",
	"en-US,en;q=0.9,de;q=0.8",
	"en-US,en;q=0.9,ja;q=0.8",
	"en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7",
	"en-US,en;q=0.9,pt;q=0.8",
	"en,en-US;q=0.9",
	"en-US,en;q=0.9,ko;q=0.8",
}

// chromeVersionMaxAge is 18 months — after this, the oldest version in the pool
// drops below ~1% market share and becomes a fingerprint signal.
// The pool was last updated Feb 2026 (Chrome 135).
// Update cadence: every 6 weeks when Chrome ships a new stable release.
const chromePoolUpdatedYear = 2026
const chromePoolUpdatedMonth = 2 // February

func init() {
	now := time.Now().UTC()
	poolAge := (now.Year()-chromePoolUpdatedYear)*12 + int(now.Month()) - chromePoolUpdatedMonth
	if poolAge >= 6 {
		// 6+ months stale — oldest versions are becoming statistically rare
		log.Printf("[WARN] Chrome version pool is %d months old (last updated %d-%02d). "+
			"Oldest version (Chrome %d) may be below 1%% market share. "+
			"Update chromeVersions in spoofer/persona.go.",
			poolAge, chromePoolUpdatedYear, chromePoolUpdatedMonth,
			chromeVersions[0].major)
	}
}

// NewPersonaManager creates a manager with the given HMAC key.
// If key is nil, a zero-key is used (still deterministic, just not secret).
func NewPersonaManager(key []byte) *PersonaManager {
	if key == nil {
		key = make([]byte, 32)
	}
	return &PersonaManager{
		key:   key,
		cache: make(map[string]*Persona),
	}
}

// Get returns the persona for a tenant on the current day (UTC).
// Cached for the lifetime of the day.
func (pm *PersonaManager) Get(tenantID string) *Persona {
	today := time.Now().UTC().Format("2006-01-02")
	cacheKey := tenantID + "|" + today

	pm.mu.RLock()
	if p, ok := pm.cache[cacheKey]; ok {
		pm.mu.RUnlock()
		return p
	}
	pm.mu.RUnlock()

	p := pm.generate(tenantID, today)

	pm.mu.Lock()
	// Evict stale entries (previous days)
	for k, v := range pm.cache {
		if v.Date != today {
			delete(pm.cache, k)
		}
	}
	pm.cache[cacheKey] = p
	pm.mu.Unlock()

	return p
}

// Apply injects the persona headers into an outbound request.
// Call this AFTER sanitizeEgressHeaders, BEFORE dispatch.
// Sets all headers a real Chrome browser would send on an API/XHR request:
// UA, Accept-Language, Client Hints (Sec-Ch-Ua-*), Sec-Fetch-*, and Accept.
func (p *Persona) Apply(h http.Header) {
	h.Set("User-Agent", p.UserAgent)
	h.Set("Accept-Language", p.AcceptLanguage)
	h.Set("Sec-Ch-Ua", p.SecChUa)
	h.Set("Sec-Ch-Ua-Platform", p.SecChUaPlatform)
	h.Set("Sec-Ch-Ua-Mobile", p.SecChUaMobile)
	// G4 fix: Chrome sends Sec-Fetch-* on every request. Their absence is a
	// fingerprint signal to Cloudflare/Akamai (JA4H cross-check).
	// Values are for API/XHR calls (cross-site fetch, cors mode, empty dest).
	h.Set("Sec-Fetch-Site", "cross-site")
	h.Set("Sec-Fetch-Mode", "cors")
	h.Set("Sec-Fetch-Dest", "empty")
	// G5 fix: Ensure Accept is set. Real API clients send application/json;
	// its absence or Go default (*/*) is a weak fingerprint.
	if h.Get("Accept") == "" {
		h.Set("Accept", "application/json")
	}
}

// generate creates a persona deterministically from HMAC(key, tenant+date).
func (pm *PersonaManager) generate(tenantID, date string) *Persona {
	mac := hmac.New(sha256.New, pm.key)
	mac.Write([]byte(tenantID + "|" + date))
	hash := mac.Sum(nil)

	// Use different bytes from the hash for each selection
	chromeIdx := pickWeighted(hash[0:4], len(chromeVersions), nil)
	osIdx := pickWeighted(hash[4:8], len(osPlatforms), osWeights())
	langIdx := pickWeighted(hash[8:12], len(acceptLanguages), nil)

	chrome := chromeVersions[chromeIdx]
	os := osPlatforms[osIdx]
	lang := acceptLanguages[langIdx]

	ua := fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
		os.ua, chrome.full)

	secChUa := fmt.Sprintf(`%s, "Google Chrome";v="%d", "Chromium";v="%d"`,
		chrome.brand, chrome.major, chrome.major)

	return &Persona{
		UserAgent:       ua,
		AcceptLanguage:  lang,
		SecChUa:         secChUa,
		SecChUaPlatform: os.chPlatform,
		SecChUaMobile:   "?0",
		Date:            date,
	}
}

// osWeights returns the weight slice for OS selection.
func osWeights() []int {
	w := make([]int, len(osPlatforms))
	for i, o := range osPlatforms {
		w[i] = o.weight
	}
	return w
}

// pickWeighted selects an index using 4 bytes of entropy.
// If weights is nil, uniform distribution.
func pickWeighted(entropy []byte, n int, weights []int) int {
	val := binary.BigEndian.Uint32(entropy)

	if weights == nil {
		return int(val % uint32(n))
	}

	total := 0
	for _, w := range weights {
		total += w
	}

	target := int(val % uint32(total))
	cumulative := 0
	for i, w := range weights {
		cumulative += w
		if target < cumulative {
			return i
		}
	}
	return n - 1
}
