package replylang

import (
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// Modes of the assistant language setting (assistant_language_mode).
const (
	// ModeAuto answers in the language the person writes in: the message's,
	// or the conversation's when a message is too short to tell. The default.
	ModeAuto = "auto"
	// ModeApp answers in the App language.
	ModeApp = "app"
	// ModeLanguage answers in the assistant_language setting.
	ModeLanguage = "language"
)

// Where a decision came from, for traces and tests.
const (
	FromRequest = "request"
	FromMessage = "message"
	// FromSpeech is the language the person spoke in, as speech to text
	// heard it with confidence (#1322).
	FromSpeech       = "speech"
	FromConversation = "conversation"
	FromSetting      = "setting"
	FromApp          = "app"
	FromSystem       = "system"
	FromDefault      = "default"
)

// Input is what the answer's language is decided from.
type Input struct {
	Mode string
	// Setting is assistant_language, used with ModeLanguage.
	Setting string
	// App is the App language (ui_locale), or "" for the system's.
	App string
	// System is this computer's language, or "".
	System string
	// Message is the person's message this turn.
	Message string
	// History is the person's earlier messages in the conversation, oldest first.
	History []string
	// Spoken is the language the message was spoken in, when speech to text
	// was confident of it, such as es (#1322). A word or two said aloud,
	// such as "¿Hola?", is too short to tell from its text.
	Spoken string
}

// Decision is the language to answer in, as a BCP 47 tag, and why.
type Decision struct {
	Tag    string
	Source string
}

// ValidMode reports a known assistant_language_mode; "" is the default.
func ValidMode(mode string) bool {
	return mode == "" || mode == ModeAuto || mode == ModeApp || mode == ModeLanguage
}

// Resolve decides the language to answer in (spec §11): a language the
// message asks for always wins. Then, by mode, the language the person
// writes or speaks in (Auto), the chosen language, or the App language; then this
// computer's language; then English.
func Resolve(in Input) Decision {
	if tag, ok := Requested(in.Message); ok {
		return Decision{tag, FromRequest}
	}
	switch in.Mode {
	case ModeLanguage:
		if tag := canonical(in.Setting); tag != "" {
			return Decision{tag, FromSetting}
		}
	case ModeApp:
		// Below.
	default:
		if tag, ok := Detect(in.Message); ok {
			return Decision{tag, FromMessage}
		}
		if tag := canonical(in.Spoken); tag != "" {
			return Decision{tag, FromSpeech}
		}
		// The conversation's recent messages, newest first.
		for i := len(in.History) - 1; i >= 0 && i >= len(in.History)-6; i-- {
			if tag, ok := Detect(in.History[i]); ok {
				return Decision{tag, FromConversation}
			}
		}
	}
	if tag := canonical(in.App); tag != "" && !pseudo(tag) {
		return Decision{tag, FromApp}
	}
	if tag := canonical(in.System); tag != "" {
		return Decision{tag, FromSystem}
	}
	return Decision{"en", FromDefault}
}

func canonical(tag string) string {
	tag = strings.ReplaceAll(strings.TrimSpace(tag), "_", "-")
	if tag == "" {
		return ""
	}
	t, err := language.Parse(tag)
	if err != nil {
		return ""
	}
	return t.String()
}

// pseudo is a pseudo-locale (en-XA, ar-XB), which answers in English.
func pseudo(tag string) bool {
	return strings.EqualFold(tag, "en-XA") || strings.EqualFold(tag, "ar-XB")
}

// Name is a language's name in English, such as "German" for de and
// "Traditional Chinese" for zh-Hant, for instructions to a model.
func Name(tag string) string {
	t, err := language.Parse(tag)
	if err != nil {
		return tag
	}
	if name := display.English.Tags().Name(t); name != "" {
		return name
	}
	return tag
}

// Instruction is the line that tells a model which language to answer in.
func (d Decision) Instruction() string {
	return "Write your reply in " + Name(d.Tag) + ", whatever language the question, sources, or earlier messages are in. " +
		"Keep code, commands, file names, and quoted text as they are."
}
