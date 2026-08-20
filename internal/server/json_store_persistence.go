package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return fmt.Errorf("save mkdir failed: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("save marshal failed: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".server-*.tmp")
	if err != nil {
		return fmt.Errorf("save temp create failed: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save temp chmod failed: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save temp write failed: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save temp sync failed: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save temp close failed: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("save replace failed: %w", err)
	}
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}
