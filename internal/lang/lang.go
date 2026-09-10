// Package lang holds what nutshell knows about each language the user can
// speak. The browser sends the language picked in its settings with every
// request, and the coding agent, the transcriber and the voice each take
// their rules from here — so no prompt has to carry the rules of every
// language at once, and adding a language is adding an entry.
package lang

import "strings"

// Language is one language together with the instructions each model needs
// to work in it. A Language nutshell has no entry for keeps only its Code:
// every prompt then falls back to language-agnostic wording rather than
// asserting rules for a language nobody wrote down.
type Language struct {
	// Code is what the browser sends, e.g. "fa".
	Code string
	// Name is the English name used inside prompts, e.g. "Persian (Farsi)".
	Name string
	// AgentRules go into the coding agent's system prompt: how a reply in
	// this language has to read.
	AgentRules string
	// STTHint tells the transcriber what to expect from the microphone.
	STTHint string
	// TTSNote tells the voice how someone speaking this language sounds.
	TTSNote string
}

// Known reports whether nutshell has rules for this language.
func (l Language) Known() bool { return l.Name != "" }

// languages holds every language with rules, by code. The UI keeps its own
// list of interface translations (internal/web/static/lang); a language
// listed there but missing here still works, it just gets no rules.
var languages = map[string]Language{
	english.Code: english,
	persian.Code: persian,
}

// Lookup returns the rules for a language code. An unknown or empty code
// comes back as a Language carrying nothing but that code.
func Lookup(code string) Language {
	if l, ok := languages[code]; ok {
		return l
	}

	return Language{Code: code}
}

// halfSpaces turns the text \u200c into real Persian half-spaces (ZWNJ).
// Written as the character itself it is invisible in the source, which hides
// a real difference between two words from anyone reading the diff, so
// staticcheck rejects it (ST1018).
func halfSpaces(s string) string { return strings.ReplaceAll(s, `\u200c`, "\u200c") }
