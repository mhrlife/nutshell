<p align="center"><img src="assets/logo.svg" alt="nutshell" width="140"></p>

# nutshell

Talk to a coding agent instead of typing at it — a question, a task, a piece
of research — and get the reply *in a nutshell*: a few spoken sentences, with
the full write-up one click away.

nutshell starts a coding agent (Claude Code today) in the current directory,
opens a small web UI on a random local port, and wires it to speech-to-text
and text-to-speech through OpenRouter.

```
you speak ──▶ OpenRouter (Gemini) ──▶ text ──▶ agent ──▶ <summary> + <full>
                                                            │
                          spoken aloud ◀── OpenRouter TTS ◀─┘
```

## Install

On Linux or macOS:

```sh
curl -fsSL https://github.com/mhrlife/nutshell/releases/latest/download/install.sh | bash
```

On Windows, in PowerShell:

```powershell
irm https://github.com/mhrlife/nutshell/releases/latest/download/install.ps1 | iex
```

The installer picks the build for your machine (Linux, macOS and Windows, on
amd64 and arm64), checks it against `checksums.txt`, drops the binary in
`~/.nutshell/bin` (`%LOCALAPPDATA%\nutshell\bin` on Windows) and puts that
directory on your `PATH`. `--version v0.1.0` pins a release, `--dir <path>`
changes where it lands and `--no-modify-path` leaves your shell config alone;
the PowerShell script takes the same options as `-Version`, `-Dir` and
`-NoModifyPath`.

Or take the archive from the
[latest release](https://github.com/mhrlife/nutshell/releases/latest) yourself:

```sh
tar xzf nutshell_0.1.0_darwin_arm64.tar.gz
sudo mv nutshell /usr/local/bin/
```

Or build it from source:

```sh
go install github.com/mhrlife/nutshell/cmd/nutshell@latest
```

You need the agent's CLI on your `PATH` (`claude`) and an OpenRouter key:

```sh
export OPENROUTER_API_KEY=sk-or-...
```

Without a key the UI still works with typed messages; voice is switched off.

## Run

```sh
cd your-project
nutshell
```

A browser tab opens on `http://127.0.0.1:<random port>`. Every nutshell you
start picks its own port, so several projects can run side by side.

Anything nutshell does not recognise is forwarded to the agent unchanged:

```sh
nutshell --model opus --mcp-config mcp.json --permission-mode acceptEdits
```

nutshell's own flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--port` | `0` (random) | Port for the web UI |
| `--no-open` | | Don't open the browser |
| `--lang` | `en` | Default UI language (`en` or `fa`) |
| `--agent` | `claude` | Which agent to drive |
| `--agent-bin` | | Path to the agent executable |
| `--openrouter-key` | `$OPENROUTER_API_KEY` | OpenRouter API key |
| `--stt-model` | `google/gemini-3.8-flash` | Speech-to-text model |
| `--tts-model` | `google/gemini-3.1-flash-tts-preview` | Text-to-speech model |
| `--tts-voice` | `Sadaltager` | Voice for the speech model |
| `--tts-prompt` | `lecture` | Delivery instructions placed before the spoken text: the built-in calm lecture style, `none`, or literal text |
| `--debug` | | Log every API call, turn and OpenRouter request (`--verbose` stays the agent's own flag) |

### When something goes wrong

Failures are never silent. The status line above the conversation says what
broke, and the same line — plus the raw error behind it — is printed by the
terminal running nutshell, including failures that happen in the browser:

```
level=ERROR msg=browser event=transcribe message="could not transcribe: openrouter 429 …"
level=ERROR msg="request failed" method=POST path=/api/transcribe status=502 error="…"
```

Run with `--debug` to also see every API call, how long each turn took, and
every request to OpenRouter.

## Using it

The window has two panes. The rail holds the conversation: each message you
send, its short spoken reply, a play button and what the message cost. The
document pane shows the full Markdown reply of the selected message. Click
any earlier message to bring its full text back.

- The **microphone button** (or **Space**) starts recording; press it again to
  send. Esc cancels. You can also type in the field next to it and press Enter.
- The **gear** opens settings:
  - **Language** picks the language of the interface *and* of the
    conversation: it travels with every request, so the agent is given that
    language's writing rules, the transcriber is told what to expect from the
    microphone, and the voice reads the answer the way that language is
    spoken. English and Persian ship; add a language by dropping a file into
    `internal/web/static/lang/`, listing it in `index.html`, and adding an
    entry with the same code to `internal/lang/` for the model-facing rules.
  - **Send as I speak** (on by default): the transcript goes straight to the
    agent. Off: the text lands in the field first so you can edit it.
  - **Read answers aloud** (on by default): every answer is spoken as it
    arrives. Off: use the play button on each answer instead.
- Settings are saved to `~/.config/nutshell/settings.json` (or the
  platform's config directory), so they survive restarts and the random port.
- Every message shows its cost; hover it for the split between speech-to-text,
  text-to-speech and the agent. The header carries the session total. A
  trailing `+` means the agent did not report its share.
- Questions and answers are shown in whatever language they were written in,
  with right-to-left layout and the Vazirmatn font for Persian.

- When the agent needs you mid-turn — permission to edit a file or run a
  command, or an answer to a question it raised — the turn stops and the
  request takes over the screen. Pick an option and the agent carries on;
  **Esc** leaves it unanswered, which the agent reads as a refusal. What you
  chose is kept as a line in the transcript.

The conversation is one continuous agent session: follow-up questions build
on earlier ones. nutshell runs the agent in `auto` permission mode — the same
mode an interactive session gets — so routine commands do not stop the turn.
Pass `--permission-mode` yourself (`plan`, `acceptEdits`, ...) to override it.

## How a reply is shaped

The agent is told to end every reply with:

```
<summary>one to three spoken sentences covering only what was asked for</summary>
<full>the complete reply in Markdown</full>
```

The summary deliberately drops most of the detail. It is meant to be heard,
not read, and it covers what was asked for rather than reporting everything
the agent did along the way. The full text keeps everything.

## Adding another agent

`internal/agent.Agent` is the contract:

```go
type Agent interface {
    Name() string
    Ask(ctx context.Context, question string, h Handler) (Answer, error)
    Cancel()
    Close() error
}

type Handler interface {
    Progress(Event)
    Prompt(ctx context.Context, p Prompt) (Reply, error)
}
```

`Handler` is the agent's way back to the user during a turn: `Progress` for
tool calls and interim text, `Prompt` for anything the agent cannot go on
without. A `Prompt` is a title, some detail and one or more questions, each
with the options it will accept; the user's `Reply` names the options they
picked. Permission requests and questions share that one shape, so an agent
that has neither concept simply never calls `Prompt`, and the server carries
both to the browser without knowing which agent it is talking to.

Implement it in `internal/agent/<name>`, reuse `agent.AnswerPrompt` and
`agent.ParseAnswer` for the answer format, and add the name to the switch in
`cmd/nutshell/main.go`. `internal/agent/claudecode` is the reference
implementation: it keeps one `claude -p --input-format stream-json` process
alive across turns, restarts it with `--resume` if it dies, and answers the
`can_use_tool` control requests that Claude Code sends when it wants the
user — which is how both permission prompts and `AskUserQuestion` arrive.

## Development

```sh
make lint   # golangci-lint + 500-line file cap
make test
make build
make icons  # regenerate the favicons from assets/logo.svg (needs ImageMagick)
```

No source file may exceed 500 lines; the linter fails otherwise.

CI runs lint, race tests and a cross-compile of every released target on
every push and pull request. Pushing a `v*` tag builds those targets and
attaches the tarballs and their checksums to a GitHub Release:

```sh
git tag v0.1.0 && git push origin v0.1.0
```
