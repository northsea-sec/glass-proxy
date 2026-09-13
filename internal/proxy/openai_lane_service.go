package proxy

import (
	"net/http"
	"strings"
)

// OpenAICompatibleLaneService owns the shared OpenAI-compatible runtime state
// used by the OpenAI and Ollama control-plane lane surfaces.
type OpenAICompatibleLaneService struct {
	upstream string
	sessions *openAISessions
}

func NewOpenAICompatibleLaneService(upstream string) *OpenAICompatibleLaneService {
	return &OpenAICompatibleLaneService{
		upstream: strings.TrimSpace(upstream),
		sessions: newOpenAISessions(),
	}
}

func normalizeOpenAICompatibleLaneLabel(lane string) string {
	switch strings.ToLower(strings.TrimSpace(lane)) {
	case "ollama":
		return "ollama"
	default:
		return "openai"
	}
}

func defaultOpenAICompatibleLaneAuthStatus(lane string) OpenAILaneAuthStatus {
	return OpenAILaneAuthStatus{
		Lane:                     normalizeOpenAICompatibleLaneLabel(lane),
		Upstream:                 "",
		UpstreamConfigured:       false,
		RequestAuthPassthrough:   true,
		AuthObserved:             false,
		LastSeenAuthType:         "unknown",
		CapturedAuthAvailable:    false,
		CapturedAuthType:         "unavailable",
		AuthlessUpstreamLikely:   false,
		SessionOwner:             "glass_openai_lane",
		SessionPersistence:       "live_memory_with_replay_template",
		SupportsStatelessProbe:   true,
		SupportsLiveSessionProbe: true,
		SupportsResumableSession: false,
		SessionCount:             0,
	}
}

func (s *OpenAICompatibleLaneService) Upstream() string {
	if s == nil {
		return ""
	}
	return s.upstream
}

func (s *OpenAICompatibleLaneService) ObserveAuth(header string) {
	if s == nil || s.sessions == nil {
		return
	}
	s.sessions.observeAuth(header)
}

func (s *OpenAICompatibleLaneService) AuthStatus(lane string) OpenAILaneAuthStatus {
	if s == nil || s.sessions == nil {
		return defaultOpenAICompatibleLaneAuthStatus(lane)
	}
	status := s.sessions.authStatus(s.upstream)
	status.Lane = normalizeOpenAICompatibleLaneLabel(lane)
	return status
}

func (s *OpenAICompatibleLaneService) Sessions() []OpenAILaneSessionSnapshot {
	if s == nil || s.sessions == nil {
		return nil
	}
	return s.sessions.snapshots()
}

func (s *OpenAICompatibleLaneService) Session(convID string) (OpenAILaneSessionSnapshot, bool) {
	if s == nil || s.sessions == nil {
		return OpenAILaneSessionSnapshot{}, false
	}
	return s.sessions.snapshot(convID)
}

func (s *OpenAICompatibleLaneService) SessionReplay(lane, convID string) (OpenAILaneSessionReplay, bool) {
	if s == nil || s.sessions == nil {
		return OpenAILaneSessionReplay{}, false
	}
	replay, ok := s.sessions.replay(convID)
	if !ok {
		return OpenAILaneSessionReplay{}, false
	}
	replay.Lane = normalizeOpenAICompatibleLaneLabel(lane)
	replay.CapturedAuthAvailable, replay.CapturedAuthType = s.sessions.capturedAuthStatus()
	return replay, true
}

func (s *OpenAICompatibleLaneService) ApplyCapturedAuth(req *http.Request) {
	if s == nil || s.sessions == nil {
		return
	}
	s.sessions.applyCapturedAuth(req)
}

func (s *OpenAICompatibleLaneService) getSession(convID string) *openAISession {
	if s == nil || s.sessions == nil {
		return nil
	}
	return s.sessions.get(convID)
}
