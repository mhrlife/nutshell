package settings_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mhrlife/nutshell/internal/settings"
)

func TestLoadMissingFileIsEmptyObject(t *testing.T) {
	t.Parallel()

	s := settings.New(filepath.Join(t.TempDir(), "nested", "settings.json"))

	doc, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}

	if string(doc) != "{}" {
		t.Errorf("Load() = %s, want {}", doc)
	}
}

func TestSaveThenLoad(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	s := settings.New(path)

	if err := s.Save(json.RawMessage(`{"lang":"fa","autoSend":false}`)); err != nil {
		t.Fatal(err)
	}

	doc, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(doc, &got); err != nil {
		t.Fatal(err)
	}

	if got["lang"] != "fa" || got["autoSend"] != false {
		t.Errorf("Load() = %v", got)
	}

	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file left behind: %v", err)
	}
}

func TestSaveRejectsNonObjects(t *testing.T) {
	t.Parallel()

	s := settings.New(filepath.Join(t.TempDir(), "settings.json"))

	for _, doc := range []string{`[]`, `"x"`, `{`, ``} {
		if err := s.Save(json.RawMessage(doc)); err == nil {
			t.Errorf("Save(%q) accepted", doc)
		}
	}
}

func TestLoadIgnoresDamagedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	doc, err := settings.New(path).Load()
	if err != nil || string(doc) != "{}" {
		t.Errorf("Load() = %s, %v; want {}", doc, err)
	}
}
