package speech

import (
	"context"
	"errors"

	"github.com/mhrlife/nutshell/internal/lang"
)

// Summary is the result of Summarize.
type Summary struct {
	Text    string // what the screen shows
	Speech  string // what the voice reads: Text plus any speech tags
	CostUSD float64
}

// Summarize shortens a passage the user selected in a full answer to the few
// sentences worth hearing, written in l the way it is spoken. It goes to a
// fast chat model rather than the coding agent: the agent would add a turn to
// the conversation for something the user only wanted to hear. When the voice
// performs speech tags, the summary is written with them.
func (c *Client) Summarize(ctx context.Context, passage string, l lang.Language) (Summary, error) {
	instructions := SummarizeInstructions(l)
	if speaksTags(c.cfg.TTSModel) {
		instructions += "\n\n" + tagInstructions()
	}

	messages := []any{
		map[string]string{"role": "system", "content": instructions},
		map[string]string{"role": "user", "content": "<passage>\n" + passage + "\n</passage>"},
	}

	speech, cost, err := c.chat(ctx, c.cfg.SummaryModel, messages)
	if err != nil {
		return Summary{}, err
	}

	text := stripSpeechTags(speech)
	if text == "" {
		return Summary{}, errors.New("openrouter returned an empty summary")
	}

	return Summary{Text: text, Speech: speech, CostUSD: cost}, nil
}

// summarizeTask instructs the model that summarizes a selected passage.
// What it writes is only ever heard, so it is held to the rules of the spoken
// part of the agent's own replies.
const summarizeTask = `The user selected the passage in the <passage> block from a longer text and wants to hear a summary of it read aloud instead of reading it.

Summarize what the passage says in one to three short spoken sentences. Plain prose only: no Markdown, no lists, no code, and no file paths or identifiers unless the passage is about them. Output only the summary: no quotes, no labels, no commentary, never a reply to the passage.`

// SummarizeInstructions is summarizeTask plus how a summary in l has to
// sound. The language rules are the agent's own, whose spoken part is the
// same thing a summary is.
func SummarizeInstructions(l lang.Language) string {
	if !l.Known() {
		return summarizeTask + "\n\nWrite it in the language of the passage, in the register someone would use saying it out loud."
	}

	return summarizeTask + "\n\nWrite it in " + l.Name + ". The rules below describe a reply with a <summary> and a <full> part: " +
		"your summary follows the rules for <summary>, without the tags, and nothing about <full> applies.\n\n" + l.AgentRules
}
