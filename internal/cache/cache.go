package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const defaultTTL = 30 * 24 * time.Hour

// Entry is a cached list of instances with metadata.
type Entry struct {
	Instances json.RawMessage `json:"instances"`
	CachedAt  time.Time       `json:"cached_at"`
	Key       string          `json:"key"`
}

func cacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".sshm", "cache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func cachePath(key string) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	// Simple sanitization: replace non-alphanumeric with _
	safe := make([]byte, 0, len(key))
	for _, c := range []byte(key) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			safe = append(safe, c)
		} else {
			safe = append(safe, '_')
		}
	}
	return filepath.Join(dir, string(safe)+".json"), nil
}

// Load returns cached data if it exists and is not expired.
// Returns nil if cache miss or expired.
func Load(key string) (json.RawMessage, error) {
	path, err := cachePath(key)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil // cache miss
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, nil // corrupted, treat as miss
	}

	if time.Since(entry.CachedAt) > defaultTTL {
		return nil, nil // expired
	}

	return entry.Instances, nil
}

// Save stores data in the cache.
func Save(key string, instances json.RawMessage) error {
	path, err := cachePath(key)
	if err != nil {
		return err
	}

	entry := Entry{
		Instances: instances,
		CachedAt:  time.Now(),
		Key:       key,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// Clear removes all cached data.
func Clear() error {
	dir, err := cacheDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // dir doesn't exist, nothing to clear
	}
	for _, e := range entries {
		os.Remove(filepath.Join(dir, e.Name()))
	}
	return nil
}
