// Package config reads nutshell's configuration file: the providers speech
// runs through, the models used on them, the agent to drive and the defaults
// the command-line flags override. Every field is optional; whatever the file
// leaves out keeps its built-in default.
package config

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// The interfaces a provider can speak.
const (
	// InterfaceOpenAI is the OpenAI API: /chat/completions and /audio/speech.
	// OpenRouter speaks it, and so do most local speech servers.
	InterfaceOpenAI = "openai"
	// InterfaceGemini is Gemini's own API: models/{model}:generateContent.
	InterfaceGemini = "gemini"
)

// The providers nutshell knows without being told.
const (
	ProviderOpenRouter = "openrouter" // every role uses it by default
	ProviderGemini     = "gemini"
)

// The modes a Gemini transcription model such as gemini-3.5-transcribe runs in.
const (
	TranscriptionSmart    = "smart"    // cleaned up for reading: no fillers or false starts
	TranscriptionVerbatim = "verbatim" // every word as it was said
)

// The routes an openai provider can serve a TTS model on.
const (
	TTSAPISpeech = "speech" // /audio/speech, the default
	TTSAPIChat   = "chat"   // /chat/completions with audio output, how LiteLLM serves Gemini TTS
)

const maxFileBytes = 1 << 20

// Config is the whole configuration file.
type Config struct {
	Port        int    `json:"port"`         // 0 takes the first free port from 4700 on
	OpenBrowser bool   `json:"open_browser"` // open the UI in a browser on start
	Debug       bool   `json:"debug"`        // log every request, turn and speech call
	Lang        string `json:"lang"`         // UI language until one is chosen in settings
	Agent       Agent  `json:"agent"`
	// Providers are the APIs speech runs through, by the name roles use.
	Providers map[string]Provider `json:"providers"`
	STT       Transcriber         `json:"stt"`     // model that transcribes speech
	Summary   Role                `json:"summary"` // chat model that shortens a passage before it is read aloud
	TTS       Voice               `json:"tts"`     // speech model that reads answers aloud
}

// Agent is the coding agent nutshell drives.
type Agent struct {
	Name string   `json:"name"` // claude, codex, or claude-wrapper
	Bin  string   `json:"bin"`  // executable; for claude-wrapper the host command and its subcommand
	Args []string `json:"args"` // passed to the agent ahead of the flags forwarded from the command line
}

// Provider is one API that speech calls go to.
type Provider struct {
	Interface string `json:"interface"` // the API it speaks: InterfaceOpenAI or InterfaceGemini
	BaseURL   string `json:"base_url"`  // API root, e.g. https://openrouter.ai/api/v1
	APIKey    string `json:"api_key"`
	// APIKeyEnv names the environment variable read when APIKey is empty.
	APIKeyEnv string `json:"api_key_env"`
}

// Role is the model a job runs on, and the provider that serves it.
type Role struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// Transcriber is the role that turns speech into text.
type Transcriber struct {
	Role

	// Transcription is the mode of a Gemini transcription model, which takes
	// the recording alone: TranscriptionSmart or TranscriptionVerbatim. Left
	// empty, see Config.TranscriptionMode.
	Transcription string `json:"transcription"`
}

// Voice is the role that reads answers aloud.
type Voice struct {
	Role

	Voice string `json:"voice"` // a voice the model knows, e.g. Charon
	Style string `json:"style"` // "dry", "none", or a literal director's note
	// API is the route an openai provider serves the model on: TTSAPISpeech
	// (the default) or TTSAPIChat. A proxy such as LiteLLM serves Gemini's TTS
	// models only through chat, and returns the audio whole, not as a stream.
	API string `json:"api"`
}

// builtinProviders are the providers nutshell knows without being told. A
// file that names one only has to give what it changes, the key above all.
// APIKeyEnv names a variable; none of them holds a credential.
func builtinProviders() map[string]Provider {
	return map[string]Provider{
		ProviderOpenRouter: { //nolint:gosec // APIKeyEnv names a variable, it holds no credential
			Interface: InterfaceOpenAI,
			BaseURL:   "https://openrouter.ai/api/v1",
			APIKeyEnv: "OPENROUTER_API_KEY",
		},
		ProviderGemini: { //nolint:gosec // APIKeyEnv names a variable, it holds no credential
			Interface: InterfaceGemini,
			BaseURL:   "https://generativelanguage.googleapis.com/v1beta",
			APIKeyEnv: "GEMINI_API_KEY",
		},
	}
}

// Default is the configuration used when there is no file.
func Default() Config {
	return Config{
		OpenBrowser: true,
		Lang:        "en",
		Agent:       Agent{Name: "claude"},
		Providers:   builtinProviders(),
		STT:         Transcriber{Role: Role{Provider: ProviderOpenRouter, Model: "google/gemini-3.8-flash"}},
		Summary:     Role{Provider: ProviderOpenRouter, Model: "google/gemini-3.8-flash"},
		TTS: Voice{
			Role:  Role{Provider: ProviderOpenRouter, Model: "google/gemini-3.1-flash-tts-preview"},
			Voice: "Charon",
			Style: "dry",
		},
	}
}

// DefaultPath is the per-user configuration file, e.g. ~/.config/nutshell/config.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: user config dir: %w", err)
	}

	return filepath.Join(dir, "nutshell", "config.json"), nil
}

// Load reads the file at path over the defaults. A missing file is reported
// as an error that matches os.ErrNotExist, together with the defaults, so the
// caller decides whether it needed one.
func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path) //nolint:gosec // the path is the user's own config file
	if err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}

	if len(data) > maxFileBytes {
		return cfg, fmt.Errorf("config: %s is larger than %d bytes", path, maxFileBytes)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return cfg, nil // a file created to be filled in later says nothing yet
	}

	if err := decode(data, &cfg); err != nil {
		return Default(), fmt.Errorf("config: %s: %w", path, err)
	}

	cfg.fillProviders()

	return cfg, nil
}

// decode reads data over cfg, refusing fields nutshell does not know: a
// misspelt key would otherwise be ignored without a word.
func decode(data []byte, cfg *Config) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	if err := dec.Decode(cfg); err != nil {
		return err
	}

	if dec.More() {
		return errors.New("unexpected data after the JSON object")
	}

	return nil
}

// fillProviders gives every provider what the file left out: for a built-in
// provider its interface, address and key variable, and for any other the
// OpenAI interface. A provider named in the file replaces the built-in entry
// as a whole, so without this "openrouter": {"api_key": "..."} would lose its
// base URL.
func (c *Config) fillProviders() {
	builtin := builtinProviders()

	for name, p := range c.Providers {
		b := builtin[name]

		p.Interface = cmp.Or(p.Interface, b.Interface, InterfaceOpenAI)
		p.BaseURL = cmp.Or(p.BaseURL, b.BaseURL)
		p.APIKeyEnv = cmp.Or(p.APIKeyEnv, b.APIKeyEnv)
		c.Providers[name] = p
	}
}

// Validate reports the first setting nutshell cannot run with.
func (c Config) Validate() error {
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("port %d is out of range", c.Port)
	}

	if c.Agent.Name == "" {
		return errors.New("agent.name is empty")
	}

	if err := c.validateProviders(); err != nil {
		return err
	}

	if err := c.validateRoles(); err != nil {
		return err
	}

	if err := c.validateTranscription(); err != nil {
		return err
	}

	return c.validateTTSAPI()
}

func (c Config) validateProviders() error {
	interfaces := []string{InterfaceOpenAI, InterfaceGemini}

	for name, p := range c.Providers {
		if !slices.Contains(interfaces, p.Interface) {
			return fmt.Errorf("providers.%s: unsupported interface %q (supported: %s)",
				name, p.Interface, strings.Join(interfaces, ", "))
		}

		if p.BaseURL == "" {
			return fmt.Errorf("providers.%s: base_url is empty", name)
		}
	}

	return nil
}

func (c Config) validateRoles() error {
	roles := []struct {
		key  string
		role Role
	}{{"stt", c.STT.Role}, {"summary", c.Summary}, {"tts", c.TTS.Role}}

	for _, r := range roles {
		if _, ok := c.Providers[r.role.Provider]; !ok {
			return fmt.Errorf("%s.provider: no provider named %q (known: %s)", r.key, r.role.Provider, c.providerNames())
		}

		if r.role.Model == "" {
			return fmt.Errorf("%s.model is empty", r.key)
		}
	}

	return nil
}

// validateTranscription checks stt.transcription, which only a Gemini
// transcription model understands.
func (c Config) validateTranscription() error {
	switch c.STT.Transcription {
	case "":
		return nil
	case TranscriptionSmart, TranscriptionVerbatim:
	default:
		return fmt.Errorf("stt.transcription: unknown mode %q (supported: %s, %s)",
			c.STT.Transcription, TranscriptionSmart, TranscriptionVerbatim)
	}

	if p := c.Providers[c.STT.Provider]; p.Interface != InterfaceGemini {
		return fmt.Errorf("stt.transcription needs a provider with the %s interface; %q speaks %s",
			InterfaceGemini, c.STT.Provider, p.Interface)
	}

	return nil
}

// TranscriptionMode is the mode stt runs a Gemini transcription model in, or
// empty for a chat model, which is given instructions with the recording. A
// model with "transcribe" in its name on a gemini provider runs smart unless
// the file says otherwise: verbatim keeps every filler and false start, and
// spells code terms out in the speaker's script ("مین دات گو" for main.go),
// which the agent cannot act on.
func (c Config) TranscriptionMode() string {
	if c.STT.Transcription != "" {
		return c.STT.Transcription
	}

	if c.Providers[c.STT.Provider].Interface == InterfaceGemini && strings.Contains(c.STT.Model, "transcribe") {
		return TranscriptionSmart
	}

	return ""
}

// validateTTSAPI checks tts.api, a choice only the OpenAI interface offers.
func (c Config) validateTTSAPI() error {
	switch c.TTS.API {
	case "", TTSAPISpeech:
		return nil
	case TTSAPIChat:
	default:
		return fmt.Errorf("tts.api: unknown route %q (supported: %s, %s)", c.TTS.API, TTSAPISpeech, TTSAPIChat)
	}

	if p := c.Providers[c.TTS.Provider]; p.Interface != InterfaceOpenAI {
		return fmt.Errorf("tts.api %q needs a provider with the %s interface; %q speaks %s",
			c.TTS.API, InterfaceOpenAI, c.TTS.Provider, p.Interface)
	}

	return nil
}

func (c Config) providerNames() string {
	names := make([]string, 0, len(c.Providers))
	for name := range c.Providers {
		names = append(names, name)
	}

	slices.Sort(names)

	return strings.Join(names, ", ")
}

// Key is the provider's API key: the one in the file, or else the one in the
// environment variable it names. getenv is normally os.Getenv.
func (p Provider) Key(getenv func(string) string) string {
	if p.APIKey != "" || p.APIKeyEnv == "" {
		return p.APIKey
	}

	return getenv(p.APIKeyEnv)
}

// Exposed reports whether the file at path holds an API key other users of
// the machine can read. Windows keeps no such permission bits.
func (c Config) Exposed(path string) bool {
	if runtime.GOOS == "windows" {
		return false
	}

	hasKey := false

	for _, p := range c.Providers {
		hasKey = hasKey || p.APIKey != ""
	}

	info, err := os.Stat(path)

	return hasKey && err == nil && info.Mode().Perm()&0o077 != 0
}
