package speech

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhrlife/nutshell/internal/lang"
)

const transcriptReply = `{"choices":[{"message":{"content":"hello"}}],"usage":{"cost":0.001}}`

// reply is how the fake OpenRouter answers one call.
type reply func(w http.ResponseWriter)

func replyStatus(code int) reply {
	return func(w http.ResponseWriter) { http.Error(w, http.StatusText(code), code) }
}

func replyBody(body string) reply {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}
}

// fakeOpenRouter returns a client talking to a server that answers the nth
// call with replies[n], repeating the last reply once they run out, and the
// number of calls it has seen.
func fakeOpenRouter(t *testing.T, replies ...reply) (*Client, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(calls.Add(1))
		replies[min(n, len(replies))-1](w)
	}))
	t.Cleanup(ts.Close)

	c := New(Config{APIKey: "test", STTModel: "stt"}, slog.New(slog.DiscardHandler))
	c.baseURL = ts.URL
	c.retryDelay = time.Millisecond

	return c, &calls
}

func TestTranscribeRetries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		replies     []reply
		wantText    string
		wantErr     bool
		unavailable bool // the error says OpenRouter could not be reached
		wantCalls   int32
	}{
		{
			name:      "a blip, then an answer",
			replies:   []reply{replyStatus(http.StatusBadGateway), replyStatus(http.StatusServiceUnavailable), replyBody(transcriptReply)},
			wantText:  "hello",
			wantCalls: 3,
		},
		{
			name:      "an error inside a 200 reply",
			replies:   []reply{replyBody(`{"error":{"code":502,"message":"upstream failed"}}`), replyBody(transcriptReply)},
			wantText:  "hello",
			wantCalls: 2,
		},
		{
			name:      "rate limited",
			replies:   []reply{replyStatus(http.StatusTooManyRequests), replyBody(transcriptReply)},
			wantText:  "hello",
			wantCalls: 2,
		},
		{
			name:        "down for every attempt",
			replies:     []reply{replyStatus(http.StatusBadGateway)},
			wantErr:     true,
			unavailable: true,
			wantCalls:   attempts,
		},
		{
			name:      "a refused key is not retried",
			replies:   []reply{replyStatus(http.StatusUnauthorized)},
			wantErr:   true,
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, calls := fakeOpenRouter(t, tt.replies...)

			got, err := c.Transcribe(t.Context(), "AAAA", "wav", lang.Language{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want an error: %v", err, tt.wantErr)
			}

			if errors.Is(err, ErrUnavailable) != tt.unavailable {
				t.Errorf("errors.Is(%v, ErrUnavailable) = %v, want %v", err, !tt.unavailable, tt.unavailable)
			}

			if got.Text != tt.wantText {
				t.Errorf("text = %q, want %q", got.Text, tt.wantText)
			}

			if n := calls.Load(); n != tt.wantCalls {
				t.Errorf("OpenRouter was called %d times, want %d", n, tt.wantCalls)
			}
		})
	}
}
