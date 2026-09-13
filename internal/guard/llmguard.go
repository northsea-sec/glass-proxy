package guard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type llmGuardScanRequest struct {
	Text string `json:"text"`
}

type llmGuardScanResponse struct {
	Allowed       bool     `json:"allowed"`
	Reasons       []string `json:"reasons"`
	SanitizedText string   `json:"sanitized_text,omitempty"`
	Warning       string   `json:"warning,omitempty"`
}

type llmGuardProcess struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	baseURL string
	ready   bool
}

func llmGuardScript() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "scripts", "llmguard_server.py")
}

func (s *Server) startLLMGuard() *llmGuardProcess {
	bind := s.cfg.LLMGuardBind
	if bind == "" {
		bind = "127.0.0.1:18901"
	}

	pythonBin := s.venvBin("python3")
	script := llmGuardScript()

	cmd := exec.Command(pythonBin, script, "--bind", bind)
	cmd.Stdout = os.Stderr // redirect to stderr so it shows in logs
	cmd.Stderr = os.Stderr

	proc := &llmGuardProcess{
		cmd:     cmd,
		baseURL: "http://" + bind,
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[GUARD-LLMGUARD] failed to start sidecar: %v", err)
		return proc
	}

	log.Printf("[GUARD-LLMGUARD] started sidecar on %s (pid %d)", bind, cmd.Process.Pid)

	// Wait for health check in background
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		for i := 0; i < 30; i++ {
			time.Sleep(time.Second)
			resp, err := client.Get(proc.baseURL + "/health")
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 200 {
					proc.mu.Lock()
					proc.ready = true
					proc.mu.Unlock()
					log.Printf("[GUARD-LLMGUARD] sidecar ready")
					return
				}
			}
		}
		log.Printf("[GUARD-LLMGUARD] sidecar failed to become ready after 30s")
	}()

	return proc
}

func (p *llmGuardProcess) isReady() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}

func (p *llmGuardProcess) stop() {
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() {
			p.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			p.cmd.Process.Kill()
		}
	}
}

func (s *Server) scanLLMGuard(ctx context.Context, text string) (bool, []string) {
	if s.llmGuardProc == nil || !s.llmGuardProc.isReady() {
		return false, nil
	}

	reqBody, _ := json.Marshal(llmGuardScanRequest{Text: text})

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "POST", s.llmGuardProc.baseURL+"/scan", bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("[GUARD-LLMGUARD] request error: %v", err)
		return false, nil
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[GUARD-LLMGUARD] HTTP error: %v", err)
		return false, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return false, nil
	}

	var scanResp llmGuardScanResponse
	if err := json.NewDecoder(resp.Body).Decode(&scanResp); err != nil {
		log.Printf("[GUARD-LLMGUARD] decode error: %v", err)
		return false, nil
	}

	if !scanResp.Allowed {
		return true, scanResp.Reasons
	}
	return false, nil
}
