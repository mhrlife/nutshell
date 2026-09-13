package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/mhrlife/nutshell/internal/cli"
	"github.com/mhrlife/nutshell/internal/config"
	"github.com/mhrlife/nutshell/internal/speech"
)

// loadConfig reads the configuration file and lays the flags over it. Without
// a file the defaults stand, unless --config named one that is not there.
func loadConfig(ctx context.Context, logger *slog.Logger, opts cli.Options) (config.Config, string, error) {
	path := opts.ConfigPath
	if path == "" {
		var err error

		if path, err = config.DefaultPath(); err != nil {
			return config.Config{}, "", err
		}
	}

	cfg, err := config.Load(path)

	switch {
	case errors.Is(err, os.ErrNotExist) && opts.ConfigPath == "":
		logger.InfoContext(ctx, "no config file, using the defaults", "path", path)
	case err != nil:
		return config.Config{}, "", err
	case cfg.Exposed(path):
		logger.WarnContext(ctx, "the config file holds an API key other users can read; chmod 600 it", "path", path)
	}

	opts.Apply(&cfg)

	if err := cfg.Validate(); err != nil {
		return config.Config{}, "", fmt.Errorf("config %s: %w", path, err)
	}

	return cfg, path, nil
}

// newSpeech builds the speech client, warning when voice has no key to run on.
func newSpeech(ctx context.Context, logger *slog.Logger, cfg config.Config, cfgPath string) *speech.Client {
	sp := speech.New(speechConfig(cfg, os.Getenv), logger)
	if !sp.Enabled() {
		logger.WarnContext(ctx, "no API key for the stt or tts provider (set api_key in the config file, or its api_key_env variable); voice is off, typing still works",
			"config", cfgPath)
	}

	return sp
}

// speechConfig resolves each speech role to its provider's address and key.
// getenv is normally os.Getenv, for keys the file leaves to the environment.
func speechConfig(cfg config.Config, getenv func(string) string) speech.Config {
	endpoint := func(r config.Role) speech.Endpoint {
		p := cfg.Providers[r.Provider]

		return speech.Endpoint{Interface: p.Interface, BaseURL: p.BaseURL, APIKey: p.Key(getenv), Model: r.Model}
	}

	return speech.Config{
		STT:           endpoint(cfg.STT.Role),
		Transcription: strings.ToUpper(cfg.TranscriptionMode()), // the API's enum is SMART, VERBATIM
		Summary:       endpoint(cfg.Summary),
		TTS:           endpoint(cfg.TTS.Role),
		Voice:         cfg.TTS.Voice,
		Style:         speech.ResolveStyle(cfg.TTS.Style),
		SpeakOverChat: cfg.TTS.API == config.TTSAPIChat,
	}
}
