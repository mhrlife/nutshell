package lang_test

import (
	"strings"
	"testing"

	"github.com/mhrlife/nutshell/internal/lang"
)

func TestLookup(t *testing.T) {
	t.Parallel()

	fa := lang.Lookup("fa")
	if !fa.Known() || fa.Name != "Persian (Farsi)" || fa.AgentRules == "" || fa.STTHint == "" || fa.TTSNote == "" {
		t.Errorf("fa = %+v", fa)
	}

	if en := lang.Lookup("en"); !en.Known() || en.AgentRules == "" {
		t.Errorf("en = %+v", en)
	}

	// A language the UI offers but nutshell has no rules for keeps its code
	// and nothing else, so every prompt can fall back on its own.
	unknown := lang.Lookup("xx")
	if unknown.Known() || unknown.Code != "xx" || unknown.AgentRules != "" || unknown.TTSNote != "" {
		t.Errorf("unknown = %+v", unknown)
	}

	if empty := lang.Lookup(""); empty.Known() {
		t.Errorf("empty code = %+v", empty)
	}
}

// TestPersianHalfSpaces guards the escape the Persian rules are written with:
// the models must receive real half-spaces, not the text \u200c, or the
// examples in the prompt spell words that do not exist.
func TestPersianHalfSpaces(t *testing.T) {
	t.Parallel()

	rules := lang.Lookup("fa").AgentRules

	if strings.Contains(rules, `\u200c`) {
		t.Error("the Persian rules still carry the unreplaced escape text")
	}

	if !strings.Contains(rules, "\u200c") {
		t.Error("the Persian rules have no half-space, so their examples are misspelled")
	}
}
