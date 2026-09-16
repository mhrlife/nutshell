// Package cli parses nutshell's command line. Flags nutshell does not own are
// forwarded to the agent untouched.
package cli

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/mhrlife/nutshell/internal/config"
)

// CommandConfigPath is `nutshell config path`: print where the configuration
// file is read from, which differs between Linux, macOS and Windows.
const CommandConfigPath = "config path"

// configCommand is the first word of the config subcommands, and the name of
// the --config flag.
const configCommand = "config"

// Options are what the command line says. Its settings are not a
// configuration of their own: Apply lays the flags that were given over the
// one read from the configuration file.
type Options struct {
	Command    string   // a subcommand to run instead of the UI, e.g. CommandConfigPath
	ConfigPath string   // --config; empty means the default file
	Version    bool     // print the version and exit
	AgentArgs  []string // everything forwarded to the agent

	overrides []func(*config.Config) // one for each flag that was given
}

// ownFlags lists the flags nutshell consumes; the value is true for flags
// that take no argument.
var ownFlags = map[string]bool{
	configCommand: false, "port": false, "no-open": true, "lang": false, "agent": false, "agent-bin": false,
	"stt-model": false, "summary-model": false, "tts-model": false, "tts-voice": false,
	"tts-prompt": false, "version": true, "debug": true, "help": true, "h": true,
}

// flagValues holds the parsed value of every setting flag.
type flagValues struct {
	port                             int
	noOpen, debug                    bool
	lang, agent, agentBin            string
	sttModel, summaryModel, ttsModel string
	ttsVoice, ttsStyle               string
}

// Parse splits args into nutshell options and agent arguments. Help output
// goes to out.
func Parse(args []string, out io.Writer) (Options, error) {
	if len(args) > 0 && args[0] == configCommand {
		return parseConfigCommand(args[1:], out)
	}

	ours, theirs := splitArgs(args)

	var (
		o Options
		v flagValues
	)

	fs := flag.NewFlagSet("nutshell", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.ConfigPath, configCommand, "", "configuration file (default: nutshell/config.json in the user config directory, e.g. ~/.config/nutshell/config.json)")
	fs.BoolVar(&o.Version, "version", false, "print the version and exit")
	register(fs, &v)
	fs.Usage = func() {
		fmt.Fprintln(out, "usage: nutshell [nutshell flags] [agent flags...]")
		fmt.Fprintln(out, "       nutshell config path   print where the configuration file is read from")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Starts a coding agent in this directory and opens a voice UI for it.")
		fmt.Fprintln(out, "Settings come from the configuration file; a flag below overrides its entry.")
		fmt.Fprintln(out, "Any flag not listed below is forwarded to the agent unchanged")
		fmt.Fprintln(out, "(for example --model, --mcp-config, --permission-mode, --add-dir).")
		fmt.Fprintln(out)
		fs.PrintDefaults()
	}

	if err := fs.Parse(ours); err != nil {
		return o, err
	}

	setters := v.setters()

	fs.Visit(func(f *flag.Flag) {
		if set, ok := setters[f.Name]; ok {
			o.overrides = append(o.overrides, set)
		}
	})

	o.AgentArgs = theirs

	return o, nil
}

// parseConfigCommand reads the words after `nutshell config`.
func parseConfigCommand(args []string, out io.Writer) (Options, error) {
	if slices.Equal(args, []string{"path"}) {
		return Options{Command: CommandConfigPath}, nil
	}

	fmt.Fprintln(out, "usage: nutshell config path")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  path   print where the configuration file is read from on this machine")

	if len(args) == 0 || slices.Contains([]string{"help", "-h", "-help", "--help"}, args[0]) {
		return Options{}, flag.ErrHelp
	}

	return Options{}, fmt.Errorf("unknown config command %q", strings.Join(args, " "))
}

// register declares the setting flags. Their defaults are the built-in ones,
// which is what help shows; a flag left out never replaces the file's value.
func register(fs *flag.FlagSet, v *flagValues) {
	def := config.Default()

	fs.IntVar(&v.port, "port", def.Port, "port for the web UI (0 takes the first free one from 4700 on, so the browser keeps what it allowed the page)")
	fs.BoolVar(&v.noOpen, "no-open", !def.OpenBrowser, "do not open the browser automatically")
	fs.BoolVar(&v.debug, "debug", def.Debug, "log every API call, turn and speech request (--verbose stays the agent's own flag)")
	fs.StringVar(&v.lang, "lang", def.Lang, "UI language code used until one is chosen in settings (the UI lists the available ones)")
	fs.StringVar(&v.agent, "agent", def.Agent.Name, "coding agent to drive: claude, codex, or claude-wrapper for a host CLI (needs --agent-bin)")
	fs.StringVar(&v.agentBin, "agent-bin", def.Agent.Bin, "path to the agent executable (default: the agent's usual name); for claude-wrapper, the host command and its subcommand, e.g. \"divar-copilot agent\"")
	fs.StringVar(&v.sttModel, "stt-model", def.STT.Model, "chat model with audio input that transcribes speech, on the stt provider")
	fs.StringVar(&v.summaryModel, "summary-model", def.Summary.Model, "model that summarizes a selected passage before it is spoken, on the summary provider")
	fs.StringVar(&v.ttsModel, "tts-model", def.TTS.Model, "text-to-speech model that speaks answers, on the tts provider")
	fs.StringVar(&v.ttsVoice, "tts-voice", def.TTS.Voice, "voice for the speech model (google/gemini-3.1-flash-tts-preview: Charon, Zephyr, Puck, Kore, Fenrir, Leda, Orus, Aoede)")
	fs.StringVar(&v.ttsStyle, "tts-prompt", def.TTS.Style, "director's note placed before the spoken text, for voices that follow one (Gemini TTS does): \"dry\" (built-in flat, fast delivery), \"none\" (send the text bare, without even the language note), or literal text")
}

// setters maps each setting flag to what it changes in the configuration.
func (v *flagValues) setters() map[string]func(*config.Config) {
	return map[string]func(*config.Config){
		"port":          func(c *config.Config) { c.Port = v.port },
		"no-open":       func(c *config.Config) { c.OpenBrowser = !v.noOpen },
		"debug":         func(c *config.Config) { c.Debug = v.debug },
		"lang":          func(c *config.Config) { c.Lang = v.lang },
		"agent":         func(c *config.Config) { c.Agent.Name = v.agent },
		"agent-bin":     func(c *config.Config) { c.Agent.Bin = v.agentBin },
		"stt-model":     func(c *config.Config) { c.STT.Model = v.sttModel },
		"summary-model": func(c *config.Config) { c.Summary.Model = v.summaryModel },
		"tts-model":     func(c *config.Config) { c.TTS.Model = v.ttsModel },
		"tts-voice":     func(c *config.Config) { c.TTS.Voice = v.ttsVoice },
		"tts-prompt":    func(c *config.Config) { c.TTS.Style = v.ttsStyle },
	}
}

// Apply lays the flags that were given over cfg, and adds the arguments
// forwarded to the agent after the ones the file lists.
func (o Options) Apply(cfg *config.Config) {
	for _, set := range o.overrides {
		set(cfg)
	}

	cfg.Agent.Args = slices.Concat(cfg.Agent.Args, o.AgentArgs)
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
