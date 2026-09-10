# nutshell

Voice-first wrapper around a coding agent (Claude Code today, others behind
the same interface). Go, standard library only; the browser UI is embedded.

## Layout

- `cmd/nutshell` – entry point: parse flags, pick the agent, start the server.
- `internal/agent` – the `Agent` interface plus the shared answer format
  (`AnswerPrompt`, `ParseAnswer`). New agents go in `internal/agent/<name>`.
- `internal/agent/claudecode` – Claude Code implementation.
- `internal/speech` – OpenRouter speech-to-text and text-to-speech.
- `internal/server` – HTTP + server-sent-events API used by the UI.
- `internal/web/static` – the UI (plain HTML/CSS/JS, no build step).

## Rules

- Run `make lint test` before finishing any change.
- No source file may exceed 500 lines (`make check-file-length`, also enforced
  by revive in `.golangci.yml`). Split files instead of suppressing.
- Always load the `golang-how-to` skill for Go work in this repo.
