// Package browser opens URLs in the user's default browser.
package browser

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrNoLauncher is returned when nothing on this machine can open a URL.
var ErrNoLauncher = errors.New("browser: no launcher found, open the URL by hand")

// Open launches the default browser for url and returns without waiting.
// The browser is detached from ctx, so stopping the caller never closes it.
func Open(ctx context.Context, url string) error {
	for _, launcher := range launchers(runtime.GOOS, os.Getenv("BROWSER")) {
		args := append(launcher[1:], url) //nolint:gocritic // launcher is a fresh slice per candidate

		cmd := exec.CommandContext(context.WithoutCancel(ctx), launcher[0], args...) //nolint:gosec // url is our own listener address
		if err := cmd.Start(); err == nil {
			go func() { _ = cmd.Wait() }() // reap the launcher once it exits

			return nil
		}
	}

	return ErrNoLauncher
}

// launchers lists commands to try, most specific first. A $BROWSER setting
// always wins; it may carry arguments separated by spaces.
func launchers(goos, browserEnv string) [][]string {
	var out [][]string

	if fields := strings.Fields(browserEnv); len(fields) > 0 {
		out = append(out, fields)
	}

	switch goos {
	case "darwin":
		out = append(out, []string{"open"})
	case "windows":
		out = append(out, []string{"rundll32", "url.dll,FileProtocolHandler"})
	default:
		out = append(out,
			[]string{"xdg-open"},
			[]string{"sensible-browser"},
			[]string{"x-www-browser"},
			[]string{"wslview"}, // WSL
		)
	}

	return out
}
