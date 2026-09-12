package agent

import (
	"regexp"
	"strings"

	"github.com/mhrlife/nutshell/internal/lang"
)

// Instructions returns what nutshell adds to the agent's system prompt so
// every final reply carries a short spoken summary and a full Markdown
// answer, written the way l is spoken. Only the rules of the language in
// front of us go in: an agent asked to keep the register of every language
// nutshell knows keeps none of them well. Implementations pass the result to
// their agent in whatever way that agent accepts extra instructions.
func Instructions(l lang.Language) string {
	return answerFormat + "\n\n" + languageRules(l) + "\n\n" + answerScope + "\n\n" + selectionNote + "\n\n" + threadNote
}

// languageRules is the part of the instructions that changes with the
// language: the one the user picked in the browser, or, for a language
// nutshell has no rules for, wording that commits to nothing beyond following
// the user.
func languageRules(l lang.Language) string {
	if !l.Known() {
		return `Both parts must be written in the language the user spoke. Never translate the user's language.

Register of <summary>: the register someone would use saying this out loud, contractions and all.
The <full> part keeps that language's normal written register instead.`
	}

	return "Both parts must be written in " + l.Name + ", which is the language the user speaks to you in. " +
		"Never answer in another language, and never translate what the user said into one.\n\n" + l.AgentRules
}

const answerFormat = `You are being used through Nutshell, a voice interface: the user speaks to you — a question, a task, a piece of research, anything — and your final reply is read aloud to them.

Every final reply MUST have exactly this structure and nothing outside it:

<summary>
...
</summary>
<full>
...
</full>

Rules for <summary>:
- Cover only what the user asked for, in one to three short spoken sentences. It is converted to speech.
- Plain prose only: no Markdown, no lists, no code, no file paths or identifiers unless the user asked about them.
- Do not try to cover everything you found. Leave out details, caveats and side findings; losing most of the detail is expected. The user can open the full answer whenever they want.

Rules for <full>:
- The complete reply with all the detail, in Markdown. Code, paths, lists and headings are welcome.`

const answerScope = `Only the final reply needs this structure; text you write before or between tool calls does not.`

var (
	summaryRe = regexp.MustCompile(`(?s)<summary>\s*(.*?)\s*</summary>`)
	fullRe    = regexp.MustCompile(`(?s)<full>\s*(.*?)\s*</full>`)
	tagRe     = regexp.MustCompile(`</?(summary|full)>`)
)

// ParseAnswer splits a reply written in the format Instructions asks for. A reply
// without the tags becomes both the full answer and the summary, so a
// misbehaving agent still produces something the UI can show.
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
