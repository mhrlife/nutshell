package server_test

import (
	"net"
	"net/http"
	"testing"
)

// Every page in the user's browser can reach the loopback port. Only the page
// nutshell opened, and clients that are not browsers, may drive the agent.
func TestGuard(t *testing.T) {
	t.Parallel()

	ts := newTestServer(&fakeAgent{})
	t.Cleanup(ts.Close) // not defer: the parallel subtests run after this function returns

	_, port, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	sameOrigin := map[string]string{"Sec-Fetch-Site": "same-origin"}

	tests := []struct {
		name   string
		host   string // Host header; empty keeps the test server's own address
		header map[string]string
		want   int
	}{
		{name: "the ui", header: sameOrigin, want: http.StatusNoContent},
		{name: "a client that is not a browser", want: http.StatusNoContent},
		{name: "localhost", host: "localhost:" + port, header: sameOrigin, want: http.StatusNoContent},
		{name: "ipv6 loopback", host: "[::1]:" + port, header: sameOrigin, want: http.StatusNoContent},
		{name: "another site", header: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusForbidden},
		{
			name:   "another site in a browser without fetch metadata",
			header: map[string]string{"Origin": "https://attacker.example"},
			want:   http.StatusForbidden,
		},
		{name: "dns rebinding", host: "attacker.example:" + port, header: sameOrigin, want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/api/cancel", http.NoBody)
			if err != nil {
				t.Fatal(err)
			}

			if tt.host != "" {
				req.Host = tt.host
			}

			for name, value := range tt.header {
				req.Header.Set(name, value)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}
