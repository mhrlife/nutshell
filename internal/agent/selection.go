package agent

import "strings"

// selectionEdge is how many characters of a selected passage's beginning and
// of its end reach the agent. The agent wrote the passage in an earlier reply,
// so its two edges are enough to find the rest there.
const selectionEdge = 200

// selectionGap stands in for the middle of a passage too long to send whole.
const selectionGap = "[…]"

// selectionNote explains the block Request.Message puts ahead of a question.
const selectionNote = `A message may open with a <selected_text> block: the user selected that passage in one of your earlier replies, as it appeared on screen with the Markdown rendered, and the rest of the message is about it. A long passage keeps only its beginning and its end, joined by ` + selectionGap + `; the whole of it is in your earlier reply.`

// Excerpt shortens a passage the user selected to its first and last
// selectionEdge characters, joined by selectionGap. A passage short enough to
// send whole comes back only trimmed.
func Excerpt(passage string) string {
	passage = strings.TrimSpace(passage)

	runes := []rune(passage)
	if len(runes) <= 2*selectionEdge {
		return passage
	}

	head := strings.TrimSpace(string(runes[:selectionEdge]))
	tail := strings.TrimSpace(string(runes[len(runes)-selectionEdge:]))

	return head + "\n" + selectionGap + "\n" + tail
}

// Message is what the agent is sent for r: the question, preceded by the
// passage it is about when there is one, and by what any side thread the user
// finished since the last question concluded.
func (r Request) Message() string {
	message := notesBlock(r.Notes)

	if passage := Excerpt(r.Selection); passage != "" {
		message += "<selected_text>\n" + passage + "\n</selected_text>\n\n"
	}

	return message + r.Text
}
