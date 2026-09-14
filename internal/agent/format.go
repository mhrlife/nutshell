package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/mhrlife/nutshell/internal/lang"
)

// Format is how an agent hands over its final reply.
type Format int

const (
	// Tagged replies are text carrying the summary and the full answer in
	// <summary> and <full> tags, split by ParseAnswer. Only the instructions
	// hold the agent to the tags, so any agent can be asked for them.
	Tagged Format = iota
	// Structured replies are an object the agent itself checks against
	// AnswerSchema, read by DecodeAnswer. An agent that can check a schema
	// asks for this one: a tagged reply that loses its tags is read aloud
	// whole.
	Structured
)

// AnswerSchema is the JSON Schema a Structured reply is held to.
const AnswerSchema = `{"type":"object",` +
	`"properties":{` +
	`"summary":{"type":"string","minLength":1,"description":"The spoken summary, read aloud."},` +
	`"full":{"type":"string","minLength":1,"description":"The complete reply in Markdown."}},` +
	`"required":["summary","full"],"additionalProperties":false}`

// Instructions returns what nutshell adds to the agent's system prompt so
// every final reply carries a short spoken summary and a full Markdown
// answer, handed over in format f and written the way l is spoken. Only the
// rules of the language in front of us go in: an agent asked to keep the
// register of every language nutshell knows keeps none of them well.
// Implementations pass the result to their agent in whatever way that agent
// accepts extra instructions.
func Instructions(l lang.Language, f Format) string {
	delivery, scope := taggedDelivery, taggedScope
	if f == Structured {
		delivery, scope = structuredDelivery, structuredScope
	}

	return voiceIntro + "\n\n" + delivery + "\n\n" + answerRules + "\n\n" + languageRules(l) + "\n\n" +
		scope + "\n\n" + selectionNote + "\n\n" + threadNote
}

// languageRules is the part of the instructions that changes with the
// language: the one the user picked in the browser, or, for a language
// nutshell has no rules for, wording that commits to nothing beyond following
// the user.
func languageRules(l lang.Language) string {
	if !l.Known() {
		return `Both parts must be written in the language the user spoke. Never translate the user's language.

Register of the summary: the register someone would use saying this out loud, contractions and all.
The full answer keeps that language's normal written register instead.`
	}

	return "Both parts must be written in " + l.Name + ", which is the language the user speaks to you in. " +
		"Never answer in another language, and never translate what the user said into one.\n\n" + l.AgentRules
}

const voiceIntro = `You are being used through Nutshell, a voice interface: the user speaks to you — a question, a task, a piece of research, anything — and your final reply is read aloud to them.`

const taggedDelivery = `Every final reply MUST have exactly this structure and nothing outside it, the summary in <summary> and the full answer in <full>:

<summary>
...
</summary>
<full>
...
</full>`

const structuredDelivery = `Every final reply is given as structured output with two fields: summary and full. The whole reply goes in those two fields; do not also write it out as text.`

const answerRules = `Rules for the summary:
- Cover only what the user asked for, in one to three short spoken sentences. It is converted to speech.
- Plain prose only: no Markdown, no lists, no code, no file paths or identifiers unless the user asked about them.
- Do not try to cover everything you found. Leave out details, caveats and side findings; losing most of the detail is expected. The user can open the full answer whenever they want.

Rules for the full answer:
- The complete reply with all the detail, in Markdown. Code, paths, lists and headings are welcome.`

const taggedScope = `Only the final reply needs this structure; text you write before or between tool calls does not.`

const structuredScope = `Only the final reply is given as structured output; text you write before or between tool calls is not part of it.`

var (
	summaryRe = regexp.MustCompile(`(?s)<summary>\s*(.*?)\s*</summary>`)
	fullRe    = regexp.MustCompile(`(?s)<full>\s*(.*?)\s*</full>`)
	tagRe     = regexp.MustCompile(`</?(summary|full)>`)
)

// ParseAnswer splits a Tagged reply. A reply without the tags becomes both the
// full answer and the summary, so a misbehaving agent still produces
// something the UI can show.
func ParseAnswer(raw string) Answer {
	a := Answer{Raw: raw}

	if m := summaryRe.FindStringSubmatch(raw); m != nil {
		a.Summary = m[1]
	}

	if m := fullRe.FindStringSubmatch(raw); m != nil {
		a.Full = m[1]
	}

	if a.Full == "" {
		a.Full = strings.TrimSpace(tagRe.ReplaceAllString(raw, ""))
	}

	if a.Summary == "" {
		a.Summary = a.Full
	}

	return a
}

// DecodeAnswer reads a Structured reply: output is the object the agent
// returned, raw the reply as the agent reported it in text.
func DecodeAnswer(raw string, output json.RawMessage) (Answer, error) {
	var parts struct {
		Summary string `json:"summary"`
		Full    string `json:"full"`
	}

	if err := json.Unmarshal(output, &parts); err != nil {
		return Answer{}, fmt.Errorf("structured answer: %w", err)
	}

	return Answer{Summary: parts.Summary, Full: parts.Full, Raw: raw}, nil
}
