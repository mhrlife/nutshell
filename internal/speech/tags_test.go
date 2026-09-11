package speech

import (
	"strings"
	"testing"
)

func TestStripSpeechTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, in, want string
	}{
		{
			name: "inline and wrapping",
			in:   "خب، [pause] کش <emphasis>فقط</emphasis> برای GET فعاله. [chuckle] <slow>حواست باشه.</slow>",
			want: "خب، کش فقط برای GET فعاله. حواست باشه.",
		},
		{name: "hyphenated", in: "[long-pause]Done <build-intensity>now</build-intensity>.", want: "Done now."},
		{name: "between words", in: "one[pause]two", want: "one two"},
		// Brackets that are not speech tags are the passage's own words.
		{name: "not a tag", in: "use [x] or <div> here", want: "use [x] or <div> here"},
		{name: "plain", in: "  nothing   to strip ", want: "nothing to strip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := stripSpeechTags(tt.in); got != tt.want {
				t.Errorf("stripSpeechTags(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Every tag the voice performs has to reach the model spelled the way the
// voice expects it, or the model invents tags that get read out.
func TestTagInstructions(t *testing.T) {
	t.Parallel()

	instructions := tagInstructions()

	for _, g := range inlineTags {
		for _, tag := range g.tags {
			if !strings.Contains(instructions, "["+tag+"]") {
				t.Errorf("instructions are missing [%s]", tag)
			}
		}
	}

	for _, g := range wrappingTags {
		for _, tag := range g.tags {
			if !strings.Contains(instructions, "<"+tag+">…</"+tag+">") {
				t.Errorf("instructions are missing <%s>", tag)
			}
		}
	}
}

func TestSpeaksTags(t *testing.T) {
	t.Parallel()

	if !speaksTags("x-ai/grok-voice-tts-1.0") {
		t.Error("grok voice should perform speech tags")
	}

	if speaksTags("google/gemini-3.1-flash-tts-preview") {
		t.Error("only grok voices are given grok's tags")
	}
}
