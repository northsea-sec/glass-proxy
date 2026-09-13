// Package itt implements inter-token timing (ITT) fingerprinting.
// Ported from mitm_itt_addon.py _detect_speculative_decoding().
//
// Analyzes SSE event timestamps to detect:
//   - Speculative decoding (burst patterns from speculation hits)
//   - Token generation rate anomalies
//   - Sycophancy scoring via timing patterns
//
// ITT fingerprinting exploits the fact that LLM token generation
// creates distinctive timing patterns based on the inference backend.
package itt

import (
	"fmt"
	"math"
	"sync"
	"time"

	"proxy.local/app/internal/sse"
)

// Sample is a single ITT measurement between consecutive tokens.
type Sample struct {
	DeltaMs   float64   // milliseconds between this token and previous
	Token     string    // the token text
	Timestamp time.Time // absolute time
}

// Fingerprint is the result of analyzing an ITT sequence.
type Fingerprint struct {
	Speculative    bool    // true if speculative decoding detected
	BurstRatio     float64 // fraction of ITTs < 10ms (speculation hits)
	MeanITT        float64 // mean inter-token time in ms
	StdITT         float64 // standard deviation
	CV             float64 // coefficient of variation (std/mean)
	MinITT         float64 // minimum ITT
	MaxITT         float64 // maximum ITT
	P50            float64 // median ITT
	P90            float64 // 90th percentile ITT
	P95            float64 // 95th percentile ITT
	P99            float64 // 99th percentile ITT
	TokenCount     int     // total tokens in this response
	TotalMs        float64 // total response time
	TokensPerSec   float64 // effective token generation rate
	SycophancyScore float64 // 0-100, higher = more sycophantic patterns
}

// Collector accumulates ITT samples for a single streaming response.
type Collector struct {
	mu        sync.Mutex
	samples   []Sample
	lastTime  time.Time
	convID    string
}

// Engine manages ITT collection across multiple concurrent responses.
type Engine struct {
	mu         sync.Mutex
	collectors map[string]*Collector // keyed by request ID
	history    []Fingerprint         // completed fingerprints
	maxHistory int
}

// NewEngine creates an ITT fingerprinting engine.
func NewEngine(maxHistory int) *Engine {
	if maxHistory <= 0 {
		maxHistory = 100
	}
	return &Engine{
		collectors: make(map[string]*Collector),
		maxHistory: maxHistory,
	}
}

// StartCollection begins ITT collection for a new streaming response.
func (e *Engine) StartCollection(requestID, convID string) *Collector {
	c := &Collector{
		convID: convID,
	}
	e.mu.Lock()
	e.collectors[requestID] = c
	e.mu.Unlock()
	return c
}

// RecordToken records a token event from SSE stream parsing.
func (c *Collector) RecordToken(evt *sse.Event) {
	token := evt.TokenDelta()
	if token == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := evt.Timestamp
	if !c.lastTime.IsZero() {
		delta := now.Sub(c.lastTime)
		c.samples = append(c.samples, Sample{
			DeltaMs:   float64(delta.Microseconds()) / 1000.0,
			Token:     token,
			Timestamp: now,
		})
	}
	c.lastTime = now
}

// FinishCollection completes ITT collection and produces a fingerprint.
func (e *Engine) FinishCollection(requestID string) *Fingerprint {
	e.mu.Lock()
	c, ok := e.collectors[requestID]
	delete(e.collectors, requestID)
	e.mu.Unlock()

	if !ok || c == nil {
		return nil
	}

	c.mu.Lock()
	samples := make([]Sample, len(c.samples))
	copy(samples, c.samples)
	c.mu.Unlock()

	fp := analyze(samples)
	if fp != nil {
		e.mu.Lock()
		e.history = append(e.history, *fp)
		if len(e.history) > e.maxHistory {
			e.history = e.history[len(e.history)-e.maxHistory:]
		}
		e.mu.Unlock()
	}

	return fp
}

// GetHistory returns recent fingerprints.
func (e *Engine) GetHistory() []Fingerprint {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]Fingerprint, len(e.history))
	copy(result, e.history)
	return result
}

// analyze computes the fingerprint from ITT samples.
// Ported from _detect_speculative_decoding() in mitm_itt_addon.py.
func analyze(samples []Sample) *Fingerprint {
	if len(samples) < 20 {
		return nil
	}

	itts := make([]float64, len(samples))
	for i, s := range samples {
		itts[i] = s.DeltaMs
	}

	// Burst ratio: fraction of ITTs < 10ms (speculation hits)
	burstCount := 0
	for _, itt := range itts {
		if itt < 10.0 {
			burstCount++
		}
	}
	burstRatio := float64(burstCount) / float64(len(itts))

	// Mean
	sum := 0.0
	for _, itt := range itts {
		sum += itt
	}
	meanITT := sum / float64(len(itts))

	if meanITT <= 0 {
		return nil
	}

	// Standard deviation
	varSum := 0.0
	for _, itt := range itts {
		d := itt - meanITT
		varSum += d * d
	}
	stdITT := math.Sqrt(varSum / float64(len(itts)))

	// Coefficient of variation
	cv := stdITT / meanITT

	// Min/Max
	minITT := itts[0]
	maxITT := itts[0]
	for _, v := range itts[1:] {
		if v < minITT {
			minITT = v
		}
		if v > maxITT {
			maxITT = v
		}
	}

	// Percentiles
	sorted := make([]float64, len(itts))
	copy(sorted, itts)
	sortFloat64s(sorted)
	p50 := percentile(sorted, 0.50)
	p90 := percentile(sorted, 0.90)
	p95 := percentile(sorted, 0.95)
	p99 := percentile(sorted, 0.99)

	// Total time
	totalMs := 0.0
	if len(samples) > 0 {
		totalMs = samples[len(samples)-1].Timestamp.Sub(samples[0].Timestamp).Seconds() * 1000
	}

	// Tokens per second
	tps := 0.0
	if totalMs > 0 {
		tps = float64(len(samples)) / (totalMs / 1000.0)
	}

	// Speculative decoding detection
	// High burst ratio (>30%) + high CV (>1.5) = speculative
	speculative := burstRatio > 0.30 && cv > 1.5

	// Sycophancy score: quick heuristic based on timing regularity
	// Very regular timing (low CV) suggests pre-generated/cached responses
	// Highly variable timing suggests genuine computation
	sycophancy := 0.0
	if cv < 0.5 {
		sycophancy = (0.5 - cv) / 0.5 * 100 // 0-100 scale
	}

	return &Fingerprint{
		Speculative:    speculative,
		BurstRatio:     burstRatio,
		MeanITT:        meanITT,
		StdITT:         stdITT,
		CV:             cv,
		MinITT:         minITT,
		MaxITT:         maxITT,
		P50:            p50,
		P90:            p90,
		P95:            p95,
		P99:            p99,
		TokenCount:     len(samples) + 1, // +1 for first token
		TotalMs:        totalMs,
		TokensPerSec:   tps,
		SycophancyScore: sycophancy,
	}
}

func (fp *Fingerprint) String() string {
	spec := "NO"
	if fp.Speculative {
		spec = "YES"
	}
	return fmt.Sprintf(
		"ITT: %d tok, %.0fms total, %.1f tok/s | mean=%.1fms p50=%.1fms p95=%.1fms cv=%.2f | burst=%.1f%% spec=%s syco=%.0f%%",
		fp.TokenCount, fp.TotalMs, fp.TokensPerSec,
		fp.MeanITT, fp.P50, fp.P95, fp.CV,
		fp.BurstRatio*100, spec, fp.SycophancyScore,
	)
}

// --- helpers ---

func sortFloat64s(a []float64) {
	// Simple insertion sort — good enough for ~1000 elements
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := p * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper || upper >= len(sorted) {
		return sorted[lower]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
