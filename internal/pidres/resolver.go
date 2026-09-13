// Package pidres resolves the PID of a local TCP client from its source port.
// Reads /proc/net/tcp to map port->inode, then scans /proc/*/fd for the owning PID.
// Only checks processes named "claude" or "node" for speed.
package pidres

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Resolver maps TCP source ports to PIDs with caching.
type Resolver struct {
	mu    sync.RWMutex
	cache map[int]cacheEntry
	ttl   time.Duration
}

type cacheEntry struct {
	pid int
	at  time.Time
}

// New creates a PID resolver with the given cache TTL.
func New(ttl time.Duration) *Resolver {
	return &Resolver{
		cache: make(map[int]cacheEntry),
		ttl:   ttl,
	}
}

// Resolve returns the PID that owns the given TCP source port on 127.0.0.1.
// Returns 0 if lookup fails.
func (r *Resolver) Resolve(srcPort int) int {
	r.mu.RLock()
	if e, ok := r.cache[srcPort]; ok && time.Since(e.at) < r.ttl {
		r.mu.RUnlock()
		return e.pid
	}
	r.mu.RUnlock()

	pid := r.resolve(srcPort)
	if pid > 0 {
		r.mu.Lock()
		r.cache[srcPort] = cacheEntry{pid: pid, at: time.Now()}
		r.mu.Unlock()
	}
	return pid
}

// ResolveAddr extracts the port from "ip:port" and resolves the PID.
func (r *Resolver) ResolveAddr(remoteAddr string) int {
	idx := strings.LastIndex(remoteAddr, ":")
	if idx < 0 {
		return 0
	}
	port, err := strconv.Atoi(remoteAddr[idx+1:])
	if err != nil {
		return 0
	}
	return r.Resolve(port)
}

func (r *Resolver) resolve(srcPort int) int {
	// Step 1: Read /proc/net/tcp, find the inode for 127.0.0.1:srcPort
	targetHex := fmt.Sprintf("0100007F:%04X", srcPort)
	inode := findInode("/proc/net/tcp", targetHex)
	if inode == "" || inode == "0" {
		// Try /proc/net/tcp6 for IPv6 loopback (::1)
		targetHex6 := fmt.Sprintf("00000000000000000000000001000000:%04X", srcPort)
		inode = findInode("/proc/net/tcp6", targetHex6)
		if inode == "" || inode == "0" {
			return 0
		}
	}

	socketStr := fmt.Sprintf("socket:[%s]", inode)

	// Step 2: Scan /proc/*/fd for the process owning this socket
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name[0] < '0' || name[0] > '9' {
			continue
		}

		// Only check processes named "claude" or "node" (CC runs as node)
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%s/comm", name))
		if err != nil {
			continue
		}
		commStr := strings.TrimSpace(string(comm))
		if commStr != "claude" && commStr != "node" {
			continue
		}

		fdDir := fmt.Sprintf("/proc/%s/fd", name)
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}

		for _, fd := range fds {
			link, err := os.Readlink(fmt.Sprintf("%s/%s", fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if link == socketStr {
				pid, _ := strconv.Atoi(name)
				return pid
			}
		}
	}

	return 0
}

func findInode(path, targetHex string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 10 && strings.EqualFold(fields[1], targetHex) {
			return fields[9]
		}
	}
	return ""
}
