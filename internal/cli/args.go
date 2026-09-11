// Package cli parses nutshell's command line. Flags nutshell does not own are
// forwarded to the agent untouched.
package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// Options are nutshell's own settings.
type Options struct {
	Port         int
	NoOpen       bool
	Version      bool
	Debug        bool
	Lang         string
	Agent        string
	AgentBin     string
	APIKey       string
	STTModel     string
	SummaryModel string
	TTSModel     string
	TTSVoice     string
	TTSPrompt    string
	TTSSpeed     float64
	AgentArgs    []string // everything forwarded to the agent
}

// ownFlags lists the flags nutshell consumes; the value is true for flags
// that take no argument.
var ownFlags = map[string]bool{
	"port": false, "no-open": true, "lang": false, "agent": false, "agent-bin": false,
	"openrouter-key": false, "stt-model": false, "summary-model": false, "tts-model": false, "tts-voice": false,
	"tts-prompt": false, "tts-speed": false, "version": true, "debug": true, "help": true, "h": true,
}

// Parse splits args into nutshell options and agent arguments. getenv
// supplies defaults (normally os.Getenv). Help output goes to out.
func Parse(args []string, getenv func(string) string, out io.Writer) (Options, error) {
	ours, theirs := splitArgs(args)

	var o Options

	fs := flag.NewFlagSet("nutshell", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.IntVar(&o.Port, "port", 0, "port for the web UI (0 picks a free one)")
	fs.BoolVar(&o.NoOpen, "no-open", false, "do not open the browser automatically")
	fs.BoolVar(&o.Version, "version", false, "print the version and exit")
	fs.BoolVar(&o.Debug, "debug", false, "log every API call, turn and OpenRouter request (--verbose stays the agent's own flag)")
	fs.StringVar(&o.Lang, "lang", "en", "UI language code used until one is chosen in settings (the UI lists the available ones)")
	fs.StringVar(&o.Agent, "agent", "claude", "coding agent to drive (claude)")
	fs.StringVar(&o.AgentBin, "agent-bin", "", "path to the agent executable (default: the agent's usual name)")
	fs.StringVar(&o.APIKey, "openrouter-key", getenv("OPENROUTER_API_KEY"), "OpenRouter API key (default $OPENROUTER_API_KEY)")
	fs.StringVar(&o.STTModel, "stt-model", "google/gemini-3.8-flash", "OpenRouter chat model with audio input that transcribes speech")
	fs.StringVar(&o.SummaryModel, "summary-model", "google/gemini-3.8-flash", "OpenRouter model that summarizes a selected passage before it is spoken")
	fs.StringVar(&o.TTSModel, "tts-model", "x-ai/grok-voice-tts-1.0", "OpenRouter text-to-speech model that speaks answers")
	fs.StringVar(&o.TTSVoice, "tts-voice", "rex", "voice for the speech model (x-ai/grok-voice-tts-1.0: eve, ara, rex, sal, leo)")
	fs.Float64Var(&o.TTSSpeed, "tts-speed", 1.2, "how fast the voice talks, 1 being the model's own pace (x-ai/grok-voice-tts-1.0: 0.7 to 1.5)")
	fs.StringVar(&o.TTSPrompt, "tts-prompt", "none", "delivery instructions placed before the spoken text, for voices that follow them (Gemini TTS does, Grok reads them aloud): \"none\" (send the text bare, without even the language note), \"lecture\" (built-in calm lecture style), or literal text")
	fs.Usage = func() {
		fmt.Fprintln(out, "usage: nutshell [nutshell flags] [agent flags...]")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Starts a coding agent in this directory and opens a voice UI for it.")
		fmt.Fprintln(out, "Any flag not listed below is forwarded to the agent unchanged")
		fmt.Fprintln(out, "(for example --model, --mcp-config, --permission-mode, --add-dir).")
		fmt.Fprintln(out)
		fs.PrintDefaults()
	}

	if err := fs.Parse(ours); err != nil {
		return o, err
	}

	o.AgentArgs = theirs

	return o, nil
}

// splitArgs separates nutshell's flags (with their values) from the rest.
func splitArgs(args []string) (ours, theirs []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" || a == "--" {
			theirs = append(theirs, a)

			continue
		}

		name := strings.TrimLeft(a, "-")

		hasValue := false

		if eq := strings.Index(name, "="); eq >= 0 {
			name = name[:eq]
			hasValue = true
		}

		isBool, mine := ownFlags[name]
		if !mine {
			theirs = append(theirs, a)

			continue
		}

		ours = append(ours, a)
		if !isBool && !hasValue && i+1 < len(args) {
			ours = append(ours, args[i+1])
			i++
		}
	}

	return ours, theirs
}
