package speech

import (
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/mhrlife/nutshell/internal/lang"
)

// The point of a streaming voice is that the first words are available while
// the rest is still being spoken, so Speak must come back with the clip
// before the reply has finished arriving.
func TestSpeakStreams(t *testing.T) {
	t.Parallel()

	read := make(chan struct{}) // the test holds the first samples

	c, _ := fakeOpenRouter(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte("ab"))
		_ = http.NewResponseController(w).Flush()

		<-read

		_, _ = w.Write([]byte("cd"))
	})

	clip, err := c.Speak(t.Context(), "hello", lang.Language{})
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}

	defer clip.Audio.Close()

	first := make([]byte, 2)
	if _, err := io.ReadFull(clip.Audio, first); err != nil {
		t.Fatalf("reading the first samples: %v", err)
	}

	if string(first) != "ab" {
		t.Errorf("first samples = %q", first)
	}

	close(read) // the voice says the rest

	rest, err := io.ReadAll(clip.Audio)
	if err != nil {
		t.Fatalf("reading the rest: %v", err)
	}

	if string(rest) != "cd" {
		t.Errorf("rest of the clip = %q", rest)
	}
}

// A provider that accepts the request and then says nothing gets another try:
// the first sample is read while retrying is still possible.
func TestSpeakRetriesSilence(t *testing.T) {
	t.Parallel()

	c, calls := fakeOpenRouter(t, func(http.ResponseWriter) {})

	if _, err := c.Speak(t.Context(), "hello", lang.Language{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want %v", err, ErrUnavailable)
	}

	if n := calls.Load(); n != attempts {
		t.Errorf("OpenRouter was called %d times, want %d", n, attempts)
	}
}
