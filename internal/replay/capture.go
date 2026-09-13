package replay

import (
	"log"
	"os"
	"sync"
)

const CaptureEnvVar = "GLASS_REPLAY_CAPTURE_DIR"

// Capture asynchronously persists request fixtures for offline replay.
// It is opt-in and enabled only when GLASS_REPLAY_CAPTURE_DIR is set.
type Capture struct {
	dir string
	ch  chan Fixture
	wg  sync.WaitGroup
}

// NewCaptureFromEnv creates a capture writer when the env var is set.
func NewCaptureFromEnv() *Capture {
	dir := os.Getenv(CaptureEnvVar)
	if dir == "" {
		return nil
	}
	c, err := NewCapture(dir)
	if err != nil {
		log.Printf("[REPLAY-CAPTURE] disabled: %v", err)
		return nil
	}
	log.Printf("[REPLAY-CAPTURE] enabled: %s", dir)
	return c
}

// NewCapture creates an async fixture writer rooted at dir.
func NewCapture(dir string) (*Capture, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	c := &Capture{
		dir: dir,
		ch:  make(chan Fixture, 128),
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for fixture := range c.ch {
			if err := WriteFixture(c.dir, fixture); err != nil {
				log.Printf("[REPLAY-CAPTURE] write failed: %v", err)
			}
		}
	}()
	return c, nil
}

// Record queues a fixture for asynchronous writing.
func (c *Capture) Record(f Fixture) {
	if c == nil {
		return
	}
	select {
	case c.ch <- f:
	default:
		log.Printf("[REPLAY-CAPTURE] dropping fixture %s: channel full", f.CaptureID)
	}
}

// Close flushes queued fixtures.
func (c *Capture) Close() {
	if c == nil {
		return
	}
	close(c.ch)
	c.wg.Wait()
}
