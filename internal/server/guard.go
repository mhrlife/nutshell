package server

// nutshell listens on a loopback port, and every page open in the user's
// browser can send requests to that port as well. Behind the API sits an
// agent that edits files and runs commands, so only the page nutshell opened,
// and clients that are not browsers at all, may use it. Two kinds of page are
// kept out:
//
//   - A page from another site. It cannot read our replies, but it can still
//     send a POST the browser does not preflight. http.CrossOriginProtection
//     refuses every state-changing request the browser marks as cross-origin.
//   - A page that pointed its own domain at 127.0.0.1. To the browser that
//     page and our port now share an origin, so the check above lets it
//     through, but its requests still name that domain in the Host header.

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

var (
	errCrossOrigin = errors.New("cross-origin requests are refused")
	errForeignHost = errors.New("requests must address the loopback interface")
)

// guard lets a request through to next only when it names a loopback host
// and is not a cross-origin write.
func (s *Server) guard(next http.Handler) http.Handler {
	crossOrigin := http.NewCrossOriginProtection()
	crossOrigin.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.writeError(w, r, http.StatusForbidden, errCrossOrigin)
	}))

	checked := crossOrigin.Handler(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			s.writeError(w, r, http.StatusForbidden, errForeignHost)

			return
		}

		checked.ServeHTTP(w, r)
	})
}

// isLoopbackHost reports whether a Host header names this machine's loopback
// interface: localhost, 127.0.0.0/8 or ::1, with or without a port.
func isLoopbackHost(host string) bool {
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}

	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") {
		return true
	}

	addr, err := netip.ParseAddr(host)

	return err == nil && addr.IsLoopback()
}
