package codex

import (
	"path/filepath"

	"proxy.local/app/internal/runtimepaths"
)

const codexStorageSubdir = "glass-proxy"

func codexHomeDir() string {
	return runtimepaths.CodexHomeDir()
}

// DefaultStorageRoot returns the Codex-owned storage root used by glass-proxy.
func DefaultStorageRoot() string {
	root := runtimepaths.CodexStorageRoot()
	if filepath.Base(root) == codexStorageSubdir {
		return root
	}
	return filepath.Join(root, codexStorageSubdir)
}

// DefaultShadowDir returns the default Codex shadow directory used by glass-proxy.
func DefaultShadowDir() string {
	return filepath.Join(DefaultStorageRoot(), "shadow")
}
