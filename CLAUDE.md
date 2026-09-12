# nutshell

Voice-first wrapper around a coding agent (Claude Code today, others behind
the same interface). Go, standard library only; the browser UI is embedded.

## Layout

- `cmd/nutshell` – entry point: parse flags, build the logger, pick the agent,
  start the server.
- `internal/agent` – the `Agent` interface plus the shared answer format
  (`Instructions`, `ParseAnswer`). New agents go in `internal/agent/<name>`.
  A question carries the `Thread` it belongs to: agents keep one conversation
  per thread, and start a thread it has not seen as a copy of its parent.
- `internal/agent/claudecode` – Claude Code implementation. `Launch` picks how
  the CLI is reached: `DirectLaunch` runs it, `WrapperLaunch` runs a host CLI
  that starts it for us. `launch.go` owns the argv for both.
- `internal/lang` – per-language rules for the agent, the transcriber and the
  voice. The browser sends its language code with every request; instructions
  are built from the entry for that code, never from all of them at once.
- `internal/speech` – OpenRouter speech-to-text and text-to-speech.
- `internal/server` – HTTP + server-sent-events API used by the UI. Every
  request passes `guard`: loopback Host only, no cross-origin writes. The tree
  of side threads lives in `threads.go`; every log entry names the thread it
  happened in, and the browser shows one thread at a time.
- `internal/web/static` – the UI (plain HTML/CSS/JS, no build step).

## Rules

- Run `make lint test` before finishing any change.
- No source file may exceed 500 lines (`make check-file-length`, also enforced
  by revive in `.golangci.yml`). Split files instead of suppressing.
- Always load the `golang-how-to` skill for Go work in this repo.
- Components take their `*slog.Logger` as a constructor argument and log with
  the `*Context` methods. Never use the `slog` package functions, never carry a
  logger in a context, and never wrap slog in a package of our own (sloglint
  enforces the first).
- Text written for a model is *instructions*; a *prompt* is something put to
  the user (`agent.Prompt`). Keep the two words apart.
