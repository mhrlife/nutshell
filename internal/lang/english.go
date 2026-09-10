package lang

// english is the language nutshell falls back to in its own interface, and
// the one most agent output is already written in.
var english = Language{
	Code: "en",
	Name: "English",
	AgentRules: `Register of <summary>: spoken English, the way a developer talks to a colleague — contractions and all, not written prose read out.
The <full> part keeps normal written English instead.`,
	STTHint: "English, with the technical vocabulary of a software developer",
	TTSNote: "The transcript is in English. Read it as a native English speaker does, with natural stress and phrasing.",
}
