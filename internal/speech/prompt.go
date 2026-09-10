package speech

// LecturePrompt is the default instruction placed before the spoken text.
// Gemini TTS reads natural-language directions ahead of the transcript and
// speaks only what follows "Transcript:".
const LecturePrompt = `Synthesize the following as a clear educational lecture.

Style:
- Neutral and professional.
- Low emotional expressiveness.
- Do not sound excited, theatrical, conversational, or overly friendly.
- Avoid exaggerated pitch changes and dramatic pauses.
- Maintain a steady rhythm and consistent volume.
- Prioritize clarity and information density over personality.

Pacing: Fast

Transcript:
`

// TranscribePrompt instructs the speech-to-text model. The code-switching
// rule is what stops a Persian sentence about the AskUserQuestion tool coming
// back as "اسک یوزر کوشن", which the agent cannot act on. A capable model
// gets this right on its own — google/gemini-3.8-flash does, 2.5-flash did
// not — so treat the rule as insurance for whatever --stt-model is pointed at,
// not as the thing carrying the feature.
const TranscribePrompt = `You are transcribing a software developer talking to a coding agent, so the
transcript has to come out machine-usable.

Write it in the language it was spoken in, but never spell an English word in
the letters of another script, however short the word is and however heavily
the speaker's accent bends it. Tool names, commands, flags, paths and library
names keep their exact English spelling and capitalisation: AskUserQuestion,
main.go, --verbose, golangci-lint. Never translate an English word either.
Words the speaker genuinely said in their own language stay in their own
script, loanwords included.

Output only the transcript with normal punctuation: no quotes, no labels, no
commentary. If the audio contains no speech, output nothing.`

// TranscribeInstruction is TranscribePrompt plus what is known about the
// speaker's language, which is free text such as "Persian (Farsi)".
func TranscribeInstruction(langHint string) string {
	if langHint == "" {
		return TranscribePrompt
	}

	return TranscribePrompt + "\n\nThe speaker most likely speaks " + langHint + "."
}

// Prompt presets selectable from the command line.
const (
	PromptLecture = "lecture"
	PromptNone    = "none"
)

// ResolvePrompt maps a --tts-prompt value to the text put before the
// transcript: a preset name, or literal text used as-is.
func ResolvePrompt(value string) string {
	switch value {
	case PromptLecture:
		return LecturePrompt
	case PromptNone, "":
		return ""
	default:
		return value + "\n"
	}
}
