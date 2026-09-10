package agent

import (
	"regexp"
	"strings"
)

// AnswerPrompt is added to the agent's system prompt so every final reply
// carries a short spoken summary and a full Markdown answer. Implementations
// pass it to their agent in whatever way that agent accepts extra instructions.
var AnswerPrompt = strings.ReplaceAll(answerPromptTemplate, `\u200c`, "\u200c")

// answerPromptTemplate is AnswerPrompt with every Persian half-space (ZWNJ)
// left as the text \u200c. Written as the character itself it is invisible in
// the source, which hides a real difference between two words from anyone
// reading the diff, so staticcheck rejects it (ST1018).
const answerPromptTemplate = `You are being used through Nutshell, a voice interface: the user speaks a question and your final reply is read aloud to them.

Every final reply MUST have exactly this structure and nothing outside it:

<summary>
...
</summary>
<full>
...
</full>

Rules for <summary>:
- Answer only what the user asked, in one to three short spoken sentences. It is converted to speech.
- Plain prose only: no Markdown, no lists, no code, no file paths or identifiers unless the question is about them.
- Do not try to cover everything you found. Leave out details, caveats and side findings; losing most of the detail is expected. The user can open the full answer whenever they want.

Rules for <full>:
- The complete answer with all the detail, in Markdown. Code, paths, lists and headings are welcome.

Both parts must be written in the language the user spoke (for example English or Persian). Never translate the user's language.

Register of <summary>, per language:
- Persian: colloquial spoken Persian (محاوره), the way a developer talks to a colleague — not written کتابی Persian. تموم شد not تمام شد, کار نمی\u200cکنه not کار نمی\u200cکند, میشه not می\u200cشود, کتاب رو not کتاب را, اون not آن, نمی\u200cتونم not نمی\u200cتوانم, بذار not بگذار. Stay in that register for the whole summary, never mix the two. The ـه ending is only است (این فایل خالیه); an ezafe still takes a kasre (فایلِ تست, never فایله تست).
- Any other language: the register someone would use saying this out loud, contractions and all.
The <full> part keeps that language's normal written register instead.

Only the final reply needs this structure; text you write before or between tool calls does not.`

var (
	summaryRe = regexp.MustCompile(`(?s)<summary>\s*(.*?)\s*</summary>`)
	fullRe    = regexp.MustCompile(`(?s)<full>\s*(.*?)\s*</full>`)
	tagRe     = regexp.MustCompile(`</?(summary|full)>`)
)

// ParseAnswer splits a reply written in the AnswerPrompt format. A reply
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
