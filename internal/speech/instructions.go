package speech

import "github.com/mhrlife/nutshell/internal/lang"

// DryStyle is the built-in director's note put before the spoken text: a
// colleague reading something out, not a narrator performing it. Only a model
// that takes directions ahead of the transcript can be sent one, such as
// google/gemini-3.1-flash-tts-preview; x-ai/grok-voice-tts-1.0 reads every
// word of a note aloud, which is what --tts-prompt=none is for.
const DryStyle = `Style: Flat affect, minimal pitch variation, dry delivery. Pace: Slightly fast conversational pace. Accent: Neutral.`

// speechBrief is the shape google/gemini-3.1-flash-tts-preview reads: the job,
// then the note on how to deliver it, and last the transcript, which is
// everything after the final header.
const speechBrief = "Read the following transcript based on the audio profile and director's note.\n" +
	"\n\n# Director's note\n"

// SpeakInstructions returns everything put before the text to speak: what the
// model is doing, the note on how to deliver it, which language the text is in
// and how that language sounds, then the header the transcript follows. An
// empty style is the user asking for no directions at all
// (--tts-prompt=none), and the text goes out bare.
func SpeakInstructions(style string, l lang.Language) string {
	if style == "" {
		return ""
	}

	note := style
	if l.TTSNote != "" {
		note += "\nLanguage: " + l.TTSNote
	}

	return speechBrief + note + "\n\n## Transcript:\n"
}

// transcribeTask instructs the speech-to-text model. The code-switching
// rule is what stops a Persian sentence about the AskUserQuestion tool coming
// back as "اسک یوزر کوشن", which the agent cannot act on. A capable model
// gets this right on its own — google/gemini-3.8-flash does, 2.5-flash did
// not — so treat the rule as insurance for whatever --stt-model is pointed at,
// not as the thing carrying the feature. Dedicated speech-to-text models take
// no instructions at all, and google/chirp-3 spells exactly those words in
// Persian letters, which is why --stt-model stays a chat model.
const transcribeTask = `You are transcribing a software developer talking to a coding agent, so the
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

// TranscribeInstructions is transcribeTask plus what the language chosen in
// the browser says to expect from the microphone. A language nutshell has no
// hint for leaves the model to work it out for itself.
func TranscribeInstructions(l lang.Language) string {
	if l.STTHint == "" {
		return transcribeTask
	}

	return transcribeTask + "\n\nThe speaker most likely speaks " + l.STTHint + "."
}

// Delivery styles selectable from the command line.
const (
	StyleDry  = "dry"
	StyleNone = "none"
)

// ResolveStyle maps a --tts-prompt value to the director's note put before
// the transcript: a preset name, or literal text used as-is.
func ResolveStyle(value string) string {
	switch value {
	case StyleDry:
		return DryStyle
	case StyleNone, "":
		return ""
	default:
		return value
	}
}
