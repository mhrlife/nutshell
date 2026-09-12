package agent

import "strings"

// A conversation is not always a line. A reply can raise a question of its
// own — what is that term, why that library — which is worth following
// without dragging the main conversation through it. So a question can open a
// side thread: a copy of the conversation as it stands, which goes its own
// way and leaves the one it came from untouched. What the side thread settled
// travels back only if the user says so, as a note on the next question the
// parent thread is asked.

// RootThread is the thread nutshell starts in, the one the user is in until
// they open a side thread.
const RootThread = "root"

// Thread is the conversation a question belongs to.
type Thread struct {
	// ID names the thread. The zero value and RootThread are the same thing.
	ID string
	// Parent is the thread this one was opened from, and is set only until
	// the side thread has a conversation of its own: an agent that has never
	// been asked anything on this thread starts it as a copy of the parent's.
	Parent string
}

// Name is the thread's ID with the zero value spelled out.
func (t Thread) Name() string {
	if t.ID == "" {
		return RootThread
	}

	return t.ID
}

// Root reports whether this is the conversation nutshell starts in.
func (t Thread) Root() bool { return t.Name() == RootThread }

// Note is what one finished side thread left for the thread it was opened
// from: the passage it started from, the questions the user asked in it, and
// the conclusion it reached. The questions travel because a conclusion on its
// own says what was settled but not what was being asked.
type Note struct {
	Title      string
	Asked      []string
	Conclusion string
}

// threadNote explains the blocks Request.Message puts ahead of a question.
const threadNote = `A message may open with one or more <side_thread> blocks. The user stepped away from this conversation into a side thread about that subject, settled it there, and is bringing back what it came to: each <asked> is a question they put to that thread, in their own words, and <settled> is what it concluded. Nothing else of that thread — none of its answers, none of its work — was ever part of this conversation or ever will be. Treat what it settled as something you established earlier, remember what they asked to get there, and carry on where this conversation left off.`

// conclusionInstruction is what nutshell asks a side thread before closing
// it, so the thread it was opened from can be told what came of it. It is
// sent as an ordinary message, so the answer arrives in the usual format and
// in the usual language.
const conclusionInstruction = `The user is done with this side thread and is going back to the conversation it was opened from.

Do not do any more work and do not use any tools. In <summary>, write two to four sentences saying what this side thread settled: the answer it arrived at and anything from it that the other conversation needs. This is the only part that travels back, so it has to stand on its own — the other conversation never saw a word of this thread. In <full>, write the same conclusion with whatever detail is worth keeping.`

// Conclusion is the message that asks a side thread to sum itself up.
func Conclusion() string { return conclusionInstruction }

// notesBlock renders the conclusions carried up from finished side threads.
func notesBlock(notes []Note) string {
	var b strings.Builder

	for _, note := range notes {
		if strings.TrimSpace(note.Conclusion) == "" {
			continue
		}

		b.WriteString("<side_thread")

		if title := oneLine(note.Title); title != "" {
			b.WriteString(` subject="` + title + `"`)
		}

		b.WriteString(">\n")

		for _, question := range note.Asked {
			if question = strings.TrimSpace(question); question != "" {
				b.WriteString("<asked>\n" + question + "\n</asked>\n")
			}
		}

		b.WriteString("<settled>\n" + strings.TrimSpace(note.Conclusion) + "\n</settled>\n</side_thread>\n\n")
	}

	return b.String()
}

// oneLine is a title fit for an attribute: one line, no quotes.
func oneLine(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	title = strings.ReplaceAll(title, `"`, "'")

	if runes := []rune(title); len(runes) > titleRunes {
		title = strings.TrimSpace(string(runes[:titleRunes])) + "…"
	}

	return title
}

// titleRunes caps how much of a side thread's subject travels with its note.
const titleRunes = 80
