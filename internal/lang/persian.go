package lang

// persian is the language nutshell was built for first: the summary is
// spoken aloud, and written Persian read out loud sounds nothing like a
// developer talking, hence the register rules.
var persian = Language{
	Code:       "fa",
	Name:       "Persian (Farsi)",
	AgentRules: halfSpaces(persianAgentRules),
	STTHint:    "Persian (Farsi), possibly mixed with English technical terms",
	TTSNote: "The transcript is in Persian (Farsi). Speak it the way an Iranian speaker does — " +
		"Persian rhythm, vowels and intonation, never Persian words read with an English accent. " +
		"English technical terms inside a Persian sentence keep their English pronunciation, said the " +
		"way an Iranian developer says them.",
}

// persianAgentRules is Language.AgentRules with every half-space (ZWNJ) left
// as the text \u200c; halfSpaces puts the real characters back, see there for
// why they cannot be written as themselves.
const persianAgentRules = `Register of <summary>: colloquial spoken Persian (محاوره), the way a developer talks to a colleague — not written کتابی Persian. تموم شد not تمام شد, کار نمی\u200cکنه not کار نمی\u200cکند, میشه not می\u200cشود, کتاب رو not کتاب را, اون not آن, نمی\u200cتونم not نمی\u200cتوانم, بذار not بگذار. Stay in that register for the whole summary, never mix the two. The ـه ending is only است (این فایل خالیه); an ezafe still takes a kasre (فایلِ تست, never فایله تست).
The <full> part is colloquial spoken Persian (محاوره) too, never کتابی: it is read aloud as well, and the user wants the same voice in both. It keeps half-spaces where Persian needs them: می\u200cشه, نمی\u200cکنه, بسته\u200cها.
Names of tools, commands, flags, paths and libraries stay in Latin script in both parts: main.go, not «مین دات گو».`
