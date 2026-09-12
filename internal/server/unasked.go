package server

// Some answers arrive without a question. The agent starts a task in the
// background, says so, and ends its turn; when that task finishes it picks the
// conversation up again by itself and answers a second time. Nobody asked for
// that answer, so there is no turn on screen to put it in — and until there is
// one, the user is told a result is coming and then never hears it.
//
// So nutshell opens a turn of its own for it, with a line of its own where the
// question would be. From there it is an ordinary turn: the same activity
// line, the same answer, the same reading aloud.

import (
	"context"
	"errors"

	"github.com/mhrlife/nutshell/internal/agent"
)

// unasked is what the agent reports the turns it took on its own to.
type unasked struct{ srv *Server }

var _ agent.Unasked = unasked{}

// Begin implements agent.Unasked.
func (u unasked) Begin(thread agent.Thread) (agent.Handler, func(agent.Answer, error)) {
	s := u.srv
	name := thread.Name()
	turn := s.session.startTurn(question{thread: name, unasked: true})

	s.logger.Debug("the agent took a turn of its own", "turn", turn, "thread", name)

	return &turnHandler{turn: turn, thread: name, log: s.session, desk: s.prompts},
		func(answer agent.Answer, err error) {
			// Nothing was asked here, so a failure leaves nothing to retry and
			// no conclusion to put back: the turn only has to stop looking
			// unfinished, which every one of these branches sees to.
			switch {
			case errors.Is(err, agent.ErrCancelled), errors.Is(err, context.Canceled):
				s.session.add(name, turn, kindError, cancelledEntry())
			case err != nil:
				s.logger.Error("a turn the agent took on its own failed", "turn", turn, "thread", name, "error", err)
				s.session.add(name, turn, kindError, map[string]string{keyMessage: err.Error()})
			default:
				s.session.add(name, turn, kindResult, answer)
			}
		}
}
