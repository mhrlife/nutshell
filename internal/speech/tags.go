package speech

import (
	"regexp"
	"strings"
)

// tagGroup is a family of speech tags as xAI's reference groups them.
type tagGroup struct {
	name string
	tags []string
}

// The delivery tags x-ai/grok-voice-tts-1.0 performs instead of reading
// aloud, from the "Speech Tags" reference at
// https://docs.x.ai/developers/model-capabilities/audio/text-to-speech.
// Inline tags stand where the sound happens; wrapping tags change how the
// words between them are said.
var (
	inlineTags = []tagGroup{
		{"pauses", []string{"pause", "long-pause", "hum-tune"}},
		{"laughter and crying", []string{"laugh", "chuckle", "giggle", "cry"}},
		{"mouth sounds", []string{"tsk", "tongue-click", "lip-smack"}},
		{"breathing", []string{"breath", "inhale", "exhale", "sigh"}},
	}
	wrappingTags = []tagGroup{
		{"volume and intensity", []string{"soft", "whisper", "loud", "build-intensity", "decrease-intensity"}},
		{"pitch and speed", []string{"higher-pitch", "lower-pitch", "slow", "fast"}},
		{"vocal style", []string{"sing-song", "singing", "emphasis"}},
	}
)

// Patterns for every tag above, wrapping tags open or closed.
var (
	inlineTagPattern   = regexp.MustCompile(`\[(?:` + tagAlternation(inlineTags) + `)\]`)
	wrappingTagPattern = regexp.MustCompile(`</?(?:` + tagAlternation(wrappingTags) + `)>`)
)

// speaksTags reports whether a text-to-speech model performs the speech tags
// rather than reading them out.
func speaksTags(model string) bool {
	return strings.HasPrefix(model, "x-ai/grok-voice-tts")
}

// tagInstructions tells a model writing for the voice which tags it may use
// and how sparingly.
func tagInstructions() string {
	return `Your summary is read by a voice that performs delivery tags instead of reading them out. Use them so it sounds like a colleague talking, not text being read: a [pause] before the point that matters, <emphasis> around the one word that carries it, <slow> for a warning, a [chuckle] or [sigh] only where a person would genuinely make that sound.

Use one to three tags in the whole summary, never one per sentence, and none when the passage gives no reason for them. Wrapping tags open and close around a whole phrase within one sentence. Tags are the only markup allowed, spelled exactly as listed:

Inline tags, placed where the sound happens: ` + tagList(inlineTags, "[%s]") + `.
Wrapping tags, around the words they change: ` + tagList(wrappingTags, "<%s>…</%s>") + `.`
}

// stripSpeechTags returns text as it reads on screen, without the tags meant
// for the voice. An inline tag may sit between two words with no space around
// it, so it leaves one behind; a wrapping tag hugs the words it wraps, so it
// leaves nothing, or "now</slow>." would read "now .".
func stripSpeechTags(text string) string {
	text = wrappingTagPattern.ReplaceAllString(inlineTagPattern.ReplaceAllString(text, " "), "")

	return strings.Join(strings.Fields(text), " ")
}

func tagAlternation(groups []tagGroup) string {
	var names []string

	for _, g := range groups {
		for _, tag := range g.tags {
			names = append(names, regexp.QuoteMeta(tag))
		}
	}

	return strings.Join(names, "|")
}

// tagList writes groups as "name: t1 t2; name: t3", each tag through format,
// whose every %s is the tag name.
func tagList(groups []tagGroup, format string) string {
	parts := make([]string, 0, len(groups))

	for _, g := range groups {
		tags := make([]string, 0, len(g.tags))
		for _, tag := range g.tags {
			tags = append(tags, strings.ReplaceAll(format, "%s", tag))
		}

		parts = append(parts, g.name+": "+strings.Join(tags, " "))
	}

	return strings.Join(parts, "; ")
}
