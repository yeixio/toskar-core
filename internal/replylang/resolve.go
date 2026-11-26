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
	// Speakable, for an answer read aloud, says whether this computer has
	// a voice for a language. A language it can't read is passed over for
	// the next choice, and the decision says which it was (#1405); nil
	// takes any language.
	Speakable func(tag string) bool
	// Offer, with Speakable, is what would add a voice for a language it
	// can't read, such as "Kitten TTS in Settings › Voice", or "".
	Offer func(tag string) string
}

// Decision is the language to answer in, as a BCP 47 tag, and why.
type Decision struct {
	Tag    string
	Source string
	// Unspoken is the language the answer would have been in, had this
	// computer a voice to read it aloud (#1405); "" when it has.
	Unspoken string
	// Offer is what would add that voice, or "".
	Offer string
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
	d := resolve(in)
	if in.Speakable == nil || in.Speakable(d.Tag) {
		return d
	}
	// No voice reads it here: the next language that has one, saying why.
	unspoken := d.Tag
	for _, next := range []Decision{
		{Tag: canonical(in.Setting), Source: FromSetting},
		{Tag: canonical(in.App), Source: FromApp},
		{Tag: canonical(in.System), Source: FromSystem},
	} {
		if next.Tag == "" || next.Tag == unspoken || pseudo(next.Tag) || (next.Source == FromSetting && in.Mode != ModeLanguage) {
			continue
		}
		if in.Speakable(next.Tag) {
			d = next
			break
		}
	}
	if d.Tag == unspoken {
		d = Decision{Tag: "en", Source: FromDefault}
	}
	d.Unspoken = unspoken
	if in.Offer != nil {
		d.Offer = in.Offer(unspoken)
	}
	return d
}

func resolve(in Input) Decision {
	if tag, ok := Requested(in.Message); ok {
		return Decision{Tag: tag, Source: FromRequest}
	}
	switch in.Mode {
	case ModeLanguage:
		if tag := canonical(in.Setting); tag != "" {
			return Decision{Tag: tag, Source: FromSetting}
		}
	case ModeApp:
		// Below.
	default:
		if tag, ok := Detect(in.Message); ok {
			return Decision{Tag: tag, Source: FromMessage}
		}
		if tag := canonical(in.Spoken); tag != "" {
			return Decision{Tag: tag, Source: FromSpeech}
		}
		// The conversation's recent messages, newest first.
		for i := len(in.History) - 1; i >= 0 && i >= len(in.History)-6; i-- {
			if tag, ok := Detect(in.History[i]); ok {
				return Decision{Tag: tag, Source: FromConversation}
			}
		}
	}
	if tag := canonical(in.App); tag != "" && !pseudo(tag) {
		return Decision{Tag: tag, Source: FromApp}
	}
	if tag := canonical(in.System); tag != "" {
		return Decision{Tag: tag, Source: FromSystem}
	}
	return Decision{Tag: "en", Source: FromDefault}
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
	line := "Write your reply in " + Name(d.Tag) + ", whatever language the question, sources, or earlier messages are in. " +
		"Keep code, commands, file names, and quoted text as they are."
	if d.Unspoken != "" {
		// The person hears the answer, and this computer has no voice for
		// their language (#1405): say so once, so the switch isn't a mystery.
		line += " Your reply is read aloud, and there is no voice here for " + Name(d.Unspoken) +
			", so start with one short sentence, in " + Name(d.Tag) + ", saying you can't speak " + Name(d.Unspoken) + " aloud yet"
		if d.Offer != "" {
			line += " and that " + d.Offer + " would add a voice for it"
		}
		line += "."
	}
	return line
}
