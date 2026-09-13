package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	cfg, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}

	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("a missing file should leave the defaults, got %+v", cfg)
	}
}

// A file someone created to fill in later is not an error.
func TestLoadEmptyFile(t *testing.T) {
	t.Parallel()

	cfg, err := Load(writeConfig(t, " \n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("an empty file should leave the defaults, got %+v", cfg)
	}
}

// A file only says what it changes; everything else keeps its default, and a
// built-in provider named for its key keeps its address.
func TestLoadOverDefaults(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{
		"lang": "fa",
		"providers": {
			"openrouter": {"api_key": "sk-or-file"},
			"openai": {"base_url": "https://api.openai.com/v1", "api_key_env": "OPENAI_API_KEY"}
		},
		"tts": {"provider": "openai", "model": "gpt-4o-mini-tts"}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	def := Default()
	if cfg.Lang != "fa" || cfg.Agent.Name != def.Agent.Name || cfg.STT != def.STT {
		t.Errorf("unexpected top-level settings: %+v", cfg)
	}

	want := Voice{Role: Role{Provider: "openai", Model: "gpt-4o-mini-tts"}, Voice: def.TTS.Voice, Style: def.TTS.Style}
	if cfg.TTS != want {
		t.Errorf("tts = %+v, want %+v", cfg.TTS, want)
	}

	or := cfg.Providers[ProviderOpenRouter]
	if or.BaseURL != def.Providers[ProviderOpenRouter].BaseURL || or.Interface != InterfaceOpenAI || or.APIKey != "sk-or-file" {
		t.Errorf("openrouter = %+v", or)
	}

	if oa := cfg.Providers["openai"]; oa.Interface != InterfaceOpenAI || oa.APIKeyEnv != "OPENAI_API_KEY" {
		t.Errorf("openai = %+v", oa)
	}
}

// A Gemini transcription model on the built-in gemini provider needs only the
// key; the interface and address come with the name.
func TestLoadGemini(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{
		"providers": {"gemini": {"api_key": "g-key"}},
		"stt": {"provider": "gemini", "model": "gemini-3.5-transcribe", "transcription": "smart"},
		"tts": {"provider": "gemini", "model": "gemini-3.1-flash-tts-preview", "voice": "Algenib"}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	g := cfg.Providers[ProviderGemini]
	if g.Interface != InterfaceGemini || g.BaseURL != Default().Providers[ProviderGemini].BaseURL || g.APIKey != "g-key" {
		t.Errorf("gemini = %+v", g)
	}

	if cfg.STT.Transcription != TranscriptionSmart || cfg.STT.Model != "gemini-3.5-transcribe" {
		t.Errorf("stt = %+v", cfg.STT)
	}
}

// The LiteLLM setup from the README: Gemini Flash for transcription and
// summaries, and Gemini TTS through chat, all on one openai provider.
func TestLoadLiteLLM(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{
		"providers": {
			"litellm": {"base_url": "https://litellm.example/v1", "api_key_env": "LITELLM_API_KEY"}
		},
		"stt": {"provider": "litellm", "model": "gemini-3.8-flash"},
		"summary": {"provider": "litellm", "model": "gemini-3.8-flash"},
		"tts": {"provider": "litellm", "model": "gemini-3.1-flash-tts-preview", "api": "chat"}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if p := cfg.Providers["litellm"]; p.Interface != InterfaceOpenAI || p.APIKeyEnv != "LITELLM_API_KEY" {
		t.Errorf("litellm = %+v", p)
	}

	if cfg.TTS.API != TTSAPIChat || cfg.TTS.Voice != Default().TTS.Voice {
		t.Errorf("tts = %+v", cfg.TTS)
	}

	if mode := cfg.TranscriptionMode(); mode != "" {
		t.Errorf("a chat model got transcription mode %q", mode)
	}
}

func TestLoadRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "an unknown key", body: `{"tts": {"voices": "x"}}`, want: "voices"},
		{name: "not JSON", body: `{"port": `, want: "config"},
		{name: "two documents", body: `{} {}`, want: "unexpected data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Load(writeConfig(t, tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*Config)
		want   string
	}{
		{name: "unknown provider", change: func(c *Config) { c.STT.Provider = "nope" }, want: `no provider named "nope"`},
		{name: "unsupported interface", change: func(c *Config) {
			c.Providers["local"] = Provider{Interface: "grpc", BaseURL: "http://localhost"}
		}, want: "unsupported interface"},
		{name: "no base url", change: func(c *Config) {
			c.Providers["local"] = Provider{Interface: InterfaceOpenAI}
		}, want: "base_url"},
		{name: "empty model", change: func(c *Config) { c.TTS.Model = "" }, want: "tts.model"},
		{name: "bad port", change: func(c *Config) { c.Port = 70000 }, want: "port"},
		{name: "transcription off gemini", change: func(c *Config) {
			c.STT.Transcription = TranscriptionSmart
		}, want: "needs a provider with the gemini interface"},
		{name: "tts over chat off openai", change: func(c *Config) {
			c.TTS.Provider, c.TTS.API = ProviderGemini, TTSAPIChat
		}, want: "needs a provider with the openai interface"},
		{name: "unknown tts api", change: func(c *Config) { c.TTS.API = "grpc" }, want: "unknown route"},
		{name: "unknown transcription mode", change: func(c *Config) {
			c.STT.Provider, c.STT.Transcription = ProviderGemini, "fast"
		}, want: "unknown mode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := Default()
			tt.change(&cfg)

			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}

	if err := Default().Validate(); err != nil {
		t.Errorf("the defaults do not validate: %v", err)
	}
}

func TestTranscriptionMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		model    string
		mode     string // stt.transcription as the file gives it
		want     string
	}{
		{name: "a transcription model runs smart", provider: ProviderGemini, model: "gemini-3.5-transcribe", want: TranscriptionSmart},
		{name: "the file can still ask for verbatim", provider: ProviderGemini, model: "gemini-3.5-transcribe", mode: TranscriptionVerbatim, want: TranscriptionVerbatim},
		{name: "a chat model on gemini is a chat model", provider: ProviderGemini, model: "gemini-3.8-flash", want: ""},
		{name: "the name means nothing off gemini", provider: ProviderOpenRouter, model: "someone/transcribe-1", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := Default()
			cfg.STT = Transcriber{Role: Role{Provider: tt.provider, Model: tt.model}, Transcription: tt.mode}

			if got := cfg.TranscriptionMode(); got != tt.want {
				t.Errorf("TranscriptionMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProviderKey(t *testing.T) {
	t.Parallel()

	getenv := func(k string) string {
		if k == "OPENROUTER_API_KEY" {
			return "env-key"
		}

		return ""
	}

	p := Default().Providers[ProviderOpenRouter]
	if got := p.Key(getenv); got != "env-key" {
		t.Errorf("without a key in the file, Key = %q, want the environment's", got)
	}

	p.APIKey = "file-key"
	if got := p.Key(getenv); got != "file-key" {
		t.Errorf("with a key in the file, Key = %q, want the file's", got)
	}
}
