package main

import (
	"log/slog"
	"net"
	"testing"
)

// hold listens on port for the rest of the test.
func hold(t *testing.T, port int) net.Listener {
	t.Helper()

	ln, err := listenOn(t.Context(), port)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	return ln
}

func portOf(t *testing.T, ln net.Listener) int {
	t.Helper()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", ln.Addr())
	}

	return addr.Port
}

// freePorts finds a port nothing listens on, with the one after it free too,
// so a test can hold the first and expect the second.
func freePorts(t *testing.T) int {
	t.Helper()

	for range 20 {
		ln, err := listenOn(t.Context(), 0)
		if err != nil {
			t.Fatal(err)
		}

		port := portOf(t, ln)
		_ = ln.Close()

		if next, err := listenOn(t.Context(), port+1); err == nil {
			_ = next.Close()

			return port
		}
	}

	t.Skip("no two free ports in a row")

	return 0
}

func TestListenFromTakesTheFirstFreePort(t *testing.T) {
	t.Parallel()

	first := freePorts(t)

	ln, ok := listenFrom(t.Context(), first, 2)
	if !ok {
		t.Fatal("no port taken")
	}

	t.Cleanup(func() { _ = ln.Close() })

	if got := portOf(t, ln); got != first {
		t.Errorf("port = %d, want %d", got, first)
	}
}

func TestListenFromSkipsATakenPort(t *testing.T) {
	t.Parallel()

	first := freePorts(t)
	hold(t, first)

	ln, ok := listenFrom(t.Context(), first, 2)
	if !ok {
		t.Fatal("no port taken")
	}

	t.Cleanup(func() { _ = ln.Close() })

	if got := portOf(t, ln); got != first+1 {
		t.Errorf("port = %d, want %d", got, first+1)
	}
}

func TestListenFromGivesUpWhenEveryPortIsTaken(t *testing.T) {
	t.Parallel()

	first := freePorts(t)
	hold(t, first)

	if ln, ok := listenFrom(t.Context(), first, 1); ok {
		_ = ln.Close()

		t.Error("took a port that is already held")
	}
}

func TestListenHonorsAnExplicitPort(t *testing.T) {
	t.Parallel()

	port := freePorts(t)

	ln, err := listen(t.Context(), slog.New(slog.DiscardHandler), port)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	if got := portOf(t, ln); got != port {
		t.Errorf("port = %d, want %d", got, port)
	}
}
