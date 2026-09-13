package claude

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"proxy.local/app/internal/glass"
)

const (
	LaneID      = "claude"
	TransportID = "anthropic_messages"
)

type ServicesConfig struct {
	Enabled                 bool
	Upstream                string
	ShadowDir               string
	PrefixWarmerIntervalSec int
	RollingSummarizer       glass.SummarizerConfig
	Transport               http.RoundTripper
}

type Services struct {
	upstream     string
	shadowDir    string
	prefixWarmer *glass.PrefixWarmer
	summarizer   *glass.RollingSummarizer
	engine       *glass.Engine
	migrateOnce  sync.Once
}

type LaneAuthStatus struct {
	Lane                     string   `json:"lane"`
	Transport                string   `json:"transport"`
	Upstream                 string   `json:"upstream"`
	UpstreamConfigured       bool     `json:"upstream_configured"`
	ShadowDir                string   `json:"shadow_dir"`
	PrefixWarmerEnabled      bool     `json:"prefix_warmer_enabled"`
	RollingSummarizerEnabled bool     `json:"rolling_summarizer_enabled"`
	RequestAuthPassthrough   bool     `json:"request_auth_passthrough"`
	CapturedAuthAvailable    bool     `json:"captured_auth_available"`
	Captured                 bool     `json:"captured"`
	AuthType                 string   `json:"auth_type"`
	CapturedAuthType         string   `json:"captured_auth_type"`
	AnthropicVersion         string   `json:"anthropic_version,omitempty"`
	Betas                    []string `json:"betas"`
	SessionOwner             string   `json:"session_owner"`
	SessionPersistence       string   `json:"session_persistence"`
	SupportsStatelessProbe   bool     `json:"supports_stateless_probe"`
	SupportsLiveSessionProbe bool     `json:"supports_live_session_probe"`
	SupportsResumableSession bool     `json:"supports_resumable_session"`
	SessionCount             int      `json:"session_count"`
}

func NewServices(cfg ServicesConfig) *Services {
	svc := &Services{
		upstream:  strings.TrimRight(strings.TrimSpace(cfg.Upstream), "/"),
		shadowDir: cfg.ShadowDir,
	}
	if !cfg.Enabled {
		return svc
	}
	svc.prefixWarmer = glass.NewPrefixWarmer(glass.PrefixWarmerConfig{
		IntervalSec: cfg.PrefixWarmerIntervalSec,
		Upstream:    svc.upstream,
		Transport:   cfg.Transport,
		ShadowDir:   cfg.ShadowDir,
	})
	svc.summarizer = glass.NewRollingSummarizer(
		cfg.RollingSummarizer,
		svc.upstream,
		cfg.ShadowDir,
		cfg.Transport,
	)
	return svc
}

func (s *Services) Attach(engine *glass.Engine) {
	if s == nil || engine == nil {
		return
	}
	s.engine = engine
	if s.prefixWarmer != nil {
		engine.SetPrefixWarmer(s.prefixWarmer)
	}
	if s.summarizer != nil {
		engine.SetSummarizer(s.summarizer)
	}
}

func (s *Services) Close() {
	if s == nil || s.prefixWarmer == nil {
		return
	}
	s.prefixWarmer.Stop()
}

func (s *Services) PrefixWarmerEnabled() bool {
	return s != nil && s.prefixWarmer != nil
}

func (s *Services) RollingSummarizerEnabled() bool {
	return s != nil && s.summarizer != nil
}

func (s *Services) AuthStatus() LaneAuthStatus {
	status := LaneAuthStatus{
		Lane:                     LaneID,
		Transport:                TransportID,
		Upstream:                 "",
		UpstreamConfigured:       false,
		ShadowDir:                "",
		PrefixWarmerEnabled:      false,
		RollingSummarizerEnabled: false,
		RequestAuthPassthrough:   true,
		CapturedAuthAvailable:    false,
		Captured:                 false,
		AuthType:                 "unavailable",
		CapturedAuthType:         "unavailable",
		Betas:                    []string{},
		SessionOwner:             "glass_claude_lane",
		SessionPersistence:       "glass_shadow_with_localcache",
		SupportsStatelessProbe:   true,
		SupportsLiveSessionProbe: true,
		SupportsResumableSession: false,
		SessionCount:             0,
	}
	if s == nil {
		return status
	}
	status.Upstream = s.upstream
	status.UpstreamConfigured = strings.TrimSpace(s.upstream) != ""
	status.ShadowDir = s.shadowDir
	status.PrefixWarmerEnabled = s.PrefixWarmerEnabled()
	status.RollingSummarizerEnabled = s.RollingSummarizerEnabled()
	status.SessionCount = s.sessionCount()
	if s.summarizer == nil {
		return status
	}
	auth := s.summarizer.AuthStatus()
	status.CapturedAuthAvailable = auth.CapturedAuthAvailable
	status.Captured = auth.Captured
	status.AuthType = auth.AuthType
	status.CapturedAuthType = auth.AuthType
	status.AnthropicVersion = auth.AnthropicVersion
	status.Betas = append([]string(nil), auth.Betas...)
	if !auth.CapturedAuthAvailable {
		status.CapturedAuthType = "unavailable"
		if auth.AuthType == "" || auth.AuthType == "missing" {
			status.AuthType = "missing"
		}
	}
	return status
}

func (s *Services) ForwardWithCapturedAuth(method, path string, payload []byte) ([]byte, int, error) {
	if s == nil || s.summarizer == nil {
		return nil, 0, fmt.Errorf("rolling summarizer unavailable")
	}
	return s.summarizer.ForwardWithCapturedAuth(method, path, payload)
}

func (s *Services) ModelsWithCapturedAuth() ([]byte, int, error) {
	return s.ForwardWithCapturedAuth(http.MethodGet, "/v1/models", nil)
}
