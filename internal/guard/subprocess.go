package guard

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

const defaultSubprocessTimeout = 30 * time.Second

func (s *Server) venvBin(name string) string {
	if s.cfg.VenvPath == "" {
		return name
	}
	return filepath.Join(s.cfg.VenvPath, "bin", name)
}

func runSubprocess(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = defaultSubprocessTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s failed: %w (stderr: %s)", name, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func runSubprocessWithStdin(ctx context.Context, timeout time.Duration, stdin []byte, name string, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = defaultSubprocessTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s failed: %w (stderr: %s)", name, err, stderr.String())
	}
	return stdout.Bytes(), nil
}
