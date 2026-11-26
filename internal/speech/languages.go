package speech

import (
	"errors"
	"sort"
	"strings"

	"github.com/yeixio/toskar-core/internal/replylang"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// voices is the Piper voice read-aloud uses for each language when no voice
// is chosen (multilingual spec §20). Each is in Piper's voices.json and uses
// a phonemizer piper-tts 1.8 ships (espeak, or plain text for Ukrainian);
// Japanese, Hebrew, and Thai voices need others, so they are left out.
var voices = map[string]string{
	"ar": "ar_JO-kareem-medium",
	"cs": "cs_CZ-jirka-medium",
	"da": "da_DK-talesyntese-medium",
	"de": "de_DE-thorsten-medium",
	"el": "el_GR-rapunzelina-low",
	"en": DefaultVoice,
	"es": "es_ES-davefx-medium",
	"fa": "fa_IR-amir-medium",
	"fi": "fi_FI-harri-medium",
	"fr": "fr_FR-siwis-medium",
	"hi": "hi_IN-pratham-medium",
	"hu": "hu_HU-anna-medium",
	"id": "id_ID-news_tts-medium",
	"it": "it_IT-paola-medium",
	"ko": "ko_KR-kss-medium",
	"nb": "no_NO-talesyntese-medium",
	"nl": "nl_NL-mls-medium",
	"pl": "pl_PL-darkman-medium",
	"pt": "pt_BR-faber-medium",
	"ru": "ru_RU-irina-medium",
	"sv": "sv_SE-nst-medium",
	"tr": "tr_TR-dfki-medium",
	"uk": "uk_UA-ukrainian_tts-medium",
	"vi": "vi_VN-vais1000-medium",
	"zh": "zh_CN-huayan-medium",
}

// whisperLanguages are the languages Whisper transcribes, and tells apart
// by itself (faster-whisper's tokenizer).
var whisperLanguages = strings.Fields(`af am ar as az ba be bg bn bo br bs ca cs cy da de el en es et eu fa fi fo fr
gl gu ha haw he hi hr ht hu hy id is it ja jw ka kk km kn ko la lb ln lo lt lv mg mi mk ml mn mr ms mt my ne nl nn
no oc pa pl ps pt ro ru sa sd si sk sl sn so sq sr su sv sw ta te tg th tk tl tr tt uk ur uz vi yi yo yue zh`)

// Hears reports a language speech to text can be told to hear, such as de.
func Hears(lang string) bool {
	if lang == "" {
		return false
	}
	for _, l := range whisperLanguages {
		if l == lang {
			return true
		}
	}
	return false
}

// VoiceFor is the voice that reads lang aloud, such as de_DE-thorsten-medium
// for de or de-AT.
func VoiceFor(lang string) (string, bool) {
	base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(lang)), "-")
	// Norwegian is one language with one voice, listed once as Bokmål
	// (#1342).
	if base == "no" || base == "nn" {
		base = "nb"
	}
	v, ok := voices[base]
	return v, ok
}

// voiceLanguages are the languages read-aloud has a voice for.
func voiceLanguages() []string {
	out := make([]string, 0, len(voices))
	for l := range voices {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// ErrNoVoice means read-aloud has no voice for the text's language.
func errNoVoice(lang string) error {
	return contracts.NewError("SPEECH_NO_VOICE", map[string]any{"language": lang},
		errors.New("there is no voice to read "+replylang.Name(lang)+" aloud yet"))
}

// voiceFor picks the voice for text: the one asked for, the one for the
// language asked for, or the one for the language the text is written in.
func voiceFor(text, voice, language string) (string, error) {
	if voice != "" {
		return voice, nil
	}
	if language == "" {
		language, _ = replylang.Detect(text)
	}
	if language == "" {
		return DefaultVoice, nil
	}
	if v, ok := VoiceFor(language); ok {
		return v, nil
	}
	return "", errNoVoice(language)
}

// CanSpeak reports whether this computer can read lang aloud in its own
// language's voice: a Piper voice, Kokoro's natural one (downloaded the
// first time it's needed), or Kitten's once installed (#1405).
func (w *Worker) CanSpeak(lang string) bool {
	if _, ok := VoiceFor(lang); ok {
		return true
	}
	if _, ok := NaturalVoiceFor(lang); ok && !w.Plain {
		return true
	}
	return w.Engine != nil && w.Engine.kittenSpeaks(lang) != ""
}

// VoiceOffer is what would add a voice for lang it can't read now, for
// the answer to say: Kitten TTS when it speaks the language and isn't
// installed, else "".
func (w *Worker) VoiceOffer(lang string) string {
	base, _, _ := strings.Cut(strings.ToLower(lang), "-")
	if !kittenSpeaksLanguage(base) || w.Engine == nil || w.Engine.kittenInstalled() {
		return ""
	}
	if w.Engine.Python != nil && w.Engine.Python.Unavailable(KittenSpec()) != "" {
		return ""
	}
	return "installing Kitten TTS in Settings › Voice"
}
