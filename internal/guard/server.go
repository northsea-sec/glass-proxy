package guard

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	cfg          ServerConfig
	mux          *http.ServeMux
	httpClient   *http.Client
	urlCache     *ttlCache[URLCheckResponse]
	contentCache *ttlCache[ContentCheckResponse]
	packageCache *ttlCache[PackageCheckResponse]
	llmGuardProc *llmGuardProcess
}

func NewServer(cfg ServerConfig) *Server {
	if strings.TrimSpace(cfg.BindAddr) == "" {
		cfg.BindAddr = DefaultBindAddr
	}
	if cfg.PackageMinAgeHours <= 0 {
		cfg.PackageMinAgeHours = DefaultPackageMinAgeHrs
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = DefaultCacheTTL
	}

	s := &Server{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
		},
		urlCache:     newTTLCache[URLCheckResponse](cfg.CacheTTL),
		contentCache: newTTLCache[ContentCheckResponse](cfg.CacheTTL),
		packageCache: newTTLCache[PackageCheckResponse](cfg.CacheTTL),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/v1/url/check", s.handleURLCheck)
	mux.HandleFunc("/v1/content/check", s.handleContentCheck)
	mux.HandleFunc("/v1/package/check", s.handlePackageCheck)
	s.mux = mux

	// Start LLM Guard sidecar if enabled
	if cfg.LLMGuardEnabled {
		s.llmGuardProc = s.startLLMGuard()
	}

	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) StopSubprocesses() {
	if s.llmGuardProc != nil {
		s.llmGuardProc.stop()
	}
}

func (s *Server) NewHTTPServer() *http.Server {
	return &http.Server{
		Addr:              s.cfg.BindAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

func (s *Server) handleURLCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req URLCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	resp := s.checkURL(r.Context(), req)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleContentCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ContentCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	resp := s.checkContent(r.Context(), req)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePackageCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PackageCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	resp := s.checkPackage(r.Context(), req)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) StartBackground(ctx context.Context) *http.Server {
	srv := s.NewHTTPServer()
	go func() {
		log.Printf("[GUARD] listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[GUARD] listen failed: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	return srv
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
