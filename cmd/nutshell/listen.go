package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
)

// The ports tried, in order, when --port is not given. A browser keeps what
// the user allowed a page (the microphone above all) per origin, and the
// port is part of the origin: starting from the same number every time means
// nutshell comes back on a port the browser already knows, where a random
// one would ask again on every start. A second nutshell running alongside
// takes the next port along, and so keeps its own.
const (
	firstPort = 4700
	portCount = 100
)

// listen opens the UI's loopback listener: on port when one was asked for,
// otherwise on the first free port from firstPort on, and only when all of
// those are taken on whatever port the system picks.
func listen(ctx context.Context, logger *slog.Logger, port int) (net.Listener, error) {
	if port != 0 {
		return listenOn(ctx, port)
	}

	if ln, ok := listenFrom(ctx, firstPort, portCount); ok {
		return ln, nil
	}

	logger.WarnContext(ctx, "every usual port is taken, using a random one",
		"from", firstPort, "to", firstPort+portCount-1)

	return listenOn(ctx, 0)
}

// listenFrom takes the first of count ports from first that can be listened on.
func listenFrom(ctx context.Context, first, count int) (net.Listener, bool) {
	for port := first; port < first+count; port++ {
		if ln, err := listenOn(ctx, port); err == nil {
			return ln, true
		}
	}

	return nil, false
}

func listenOn(ctx context.Context, port int) (net.Listener, error) {
	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("listening: %w", err)
	}

	return ln, nil
}
