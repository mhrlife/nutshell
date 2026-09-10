// Package settings persists the UI's preferences between launches. The UI
// owns the shape of the document; the store only guarantees it is a JSON
// object of reasonable size and writes it atomically.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	maxDocumentBytes = 64 << 10
	dirPerm          = 0o700
	filePerm         = 0o600
)

// ErrNotObject is returned by Save for anything but a JSON object.
var ErrNotObject = errors.New("settings: document must be a JSON object")

// Store keeps one JSON document in a file.
type Store struct {
	mu   sync.Mutex
	path string
}

// New returns a store backed by path. The file is created on first Save.
func New(path string) *Store {
	return &Store{path: path}
}

// DefaultPath is the per-user settings file, e.g. ~/.config/nutshell/settings.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("settings: user config dir: %w", err)
	}

	return filepath.Join(dir, "nutshell", "settings.json"), nil
}

// Path returns the file the store reads and writes.
func (s *Store) Path() string { return s.path }

// Load returns the stored document, or an empty object when there is none.
func (s *Store) Load() (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return json.RawMessage("{}"), nil
	}

	if err != nil {
		return nil, fmt.Errorf("settings: read: %w", err)
	}

	if !isObject(data) {
		return json.RawMessage("{}"), nil // a damaged file should not brick the UI
	}

	return json.RawMessage(data), nil
}

// Save replaces the stored document.
func (s *Store) Save(doc json.RawMessage) error {
	if len(doc) > maxDocumentBytes {
		return errors.New("settings: document too large")
	}

	if !isObject(doc) {
		return ErrNotObject
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), dirPerm); err != nil {
		return fmt.Errorf("settings: create dir: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, doc, filePerm); err != nil {
		return fmt.Errorf("settings: write: %w", err)
	}

	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("settings: replace: %w", err)
	}

	return nil
}

func isObject(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}

	var probe map[string]json.RawMessage

	return json.Unmarshal(trimmed, &probe) == nil
}
