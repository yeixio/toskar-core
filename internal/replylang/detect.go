// Package replylang decides the language an answer is written in
// (multilingual spec §11–12, §30): a language the person asks for, the one
// they write in, the assistant language setting, or the App language.
// Detection runs here, on this computer; nothing is sent anywhere to find
// out what language a message is in.
package replylang

import (
	"strings"
	"unicode"
)

// minWords is how many words a message in a Latin-script language needs
// before its language is guessed. "ok" or "thanks!" say nothing.
const minWords = 3

// Detect guesses the language a text is written in, as a BCP 47 tag such as
// "de" or "zh-Hant". ok is false when the text is too short, mixed, or
// mostly code to tell.
func Detect(text string) (tag string, ok bool) {
	text = stripCode(text)
	if tag, ok := byScript(text); ok {
		return tag, true
	}
	return byWords(text)
}

// stripCode drops fenced and inline code, URLs, and file paths, which are
// written the same way in every language.
func stripCode(text string) string {
	var b strings.Builder
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		parts := strings.Split(line, "`")
		for i, part := range parts {
			if i%2 == 0 {
				b.WriteString(part)
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}
	words := strings.Fields(b.String())
	kept := words[:0]
	for _, w := range words {
		if strings.Contains(w, "://") || strings.Count(w, "/") > 1 || strings.HasPrefix(w, "~/") {
			continue
		}
		kept = append(kept, w)
	}
	return strings.Join(kept, " ")
}

// byScript recognizes languages by their writing system. Chinese is told
// apart from Japanese by kana, and Simplified from Traditional by characters
// that differ between them.
func byScript(text string) (string, bool) {
	counts := map[string]int{}
	letters := 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			counts["kana"]++
		case unicode.Is(unicode.Hangul, r):
			counts["ko"]++
		case unicode.Is(unicode.Han, r):
			counts["han"]++
		case unicode.Is(unicode.Cyrillic, r):
			counts["cyrillic"]++
		case unicode.Is(unicode.Arabic, r):
			counts["arabic"]++
		case unicode.Is(unicode.Hebrew, r):
			counts["he"]++
		case unicode.Is(unicode.Greek, r):
			counts["el"]++
		case unicode.Is(unicode.Thai, r):
			counts["th"]++
		case unicode.Is(unicode.Devanagari, r):
			counts["hi"]++
		}
	}
	if letters == 0 {
		return "", false
	}
	// A script counts when it is a real part of the text, not a name or a
	// symbol in an English sentence.
	share := func(n int) bool { return n >= 2 && n*4 >= letters }
	switch {
	case counts["kana"] > 0 && share(counts["kana"]+counts["han"]):
		return "ja", true
	case share(counts["ko"]):
		return "ko", true
	case share(counts["han"]):
		return chineseScript(text), true
	case share(counts["cyrillic"]):
		if strings.ContainsAny(strings.ToLower(text), "іїєґ") {
			return "uk", true
		}
		return "ru", true
	case share(counts["arabic"]):
		if strings.ContainsAny(text, "ٹڈڑںے") {
			return "ur", true
		}
		if strings.ContainsAny(text, "پچژگی") {
			return "fa", true
		}
		return "ar", true
	}
	for _, s := range []string{"he", "el", "th", "hi"} {
		if share(counts[s]) {
			return s, true
		}
	}
	return "", false
}

// Characters common in everyday writing that differ between Simplified and
// Traditional Chinese, such as 这/這 and 们/們.
const (
	simplifiedOnly  = "这们个说来时会国对为过还没问题样与发开关见现实经长东车门话书买卖请让认识间听学觉写边题电脑机网号码设备么"
	traditionalOnly = "這們個說來時會國對為過還沒問題樣與發開關見現實經長東車門話書買賣請讓認識間聽學覺寫邊題電腦機網號碼設備麼"
)

func chineseScript(text string) string {
	simplified, traditional := 0, 0
	for _, r := range text {
		if strings.ContainsRune(simplifiedOnly, r) {
			simplified++
		}
		if strings.ContainsRune(traditionalOnly, r) {
			traditional++
		}
	}
	if traditional > simplified {
		return "zh-Hant"
	}
	return "zh-Hans"
}

// Function words that are common in each language and rare in the others.
// A word shared by two languages counts for both; the words only one of
// them uses decide.
var functionWords = map[string][]string{
	"en": {"a", "an", "in", "on", "be", "when", "where", "who", "why", "more", "could", "should", "the", "and", "is", "are", "you", "what", "how", "this", "that", "with", "for", "can", "please", "my", "your", "of", "to", "it", "do", "does", "i", "me", "have", "was", "will", "would", "about", "from", "which", "there"},
	"de": {"den", "dem", "des", "im", "ist", "sind", "welche", "welcher", "wann", "mehr", "kann", "eigentlich", "genau", "der", "die", "das", "und", "ist", "nicht", "ich", "du", "sie", "wie", "was", "mit", "für", "auf", "ein", "eine", "einen", "bitte", "mir", "mein", "dein", "kannst", "können", "habe", "hast", "warum", "auch", "noch", "oder", "wenn", "aber", "zu", "von"},
	"es": {"lo", "la", "de", "un", "cuál", "cuándo", "más", "hacer", "puedo", "tiene", "ser", "el", "los", "las", "es", "y", "que", "qué", "cómo", "por", "para", "con", "una", "pero", "muy", "también", "puedes", "hola", "gracias", "sí", "porque", "esto", "eso", "mi", "tu", "del", "al", "está", "son", "hay", "dónde", "cuál", "yo", "me", "sobre", "si", "favor", "usted", "necesita", "necesito", "quiero", "puede", "ayudar", "ayuda", "pregúnteme", "dígame", "también", "ahora", "bien",
		"no", "en", "se", "su", "le", "nos", "tengo", "tienes", "tiene", "esta", "este", "estás", "estará", "hace", "mucho", "mucha", "unos", "unas", "después", "noche", "buenos", "buenas", "días", "claro", "nada", "todo", "bueno", "aquí", "ya", "donde", "cuando"},
	"fr": {"la", "de", "un", "quel", "quelle", "quels", "plus", "au", "aux", "ou", "où", "sont", "faire", "peux", "pouvez", "est-ce", "ça", "le", "les", "est", "et", "je", "tu", "vous", "nous", "pas", "que", "qui", "quoi", "comment", "pour", "avec", "une", "des", "du", "mais", "très", "aussi", "bonjour", "merci", "oui", "parce", "ce", "cette", "mon", "ton", "votre", "sur", "dans", "il", "elle", "c'est", "j'ai", "en"},
	"it": {"la", "più", "quale", "quando", "fare", "posso", "ha", "essere", "ci", "sei", "il", "lo", "gli", "le", "è", "e", "che", "come", "per", "con", "una", "ma", "molto", "anche", "ciao", "grazie", "sì", "perché", "questo", "questa", "mio", "tuo", "del", "della", "sono", "non", "puoi", "cosa", "dove", "io", "mi", "di", "un"},
	"pt": {"a", "de", "qual", "quando", "mais", "fazer", "posso", "tem", "ser", "ao", "o", "os", "as", "é", "e", "que", "como", "por", "para", "com", "uma", "mas", "muito", "também", "olá", "obrigado", "obrigada", "sim", "não", "você", "isso", "este", "esta", "meu", "seu", "do", "da", "dos", "das", "em", "no", "na", "está", "são", "pode", "eu", "um"},
	"nl": {"de", "het", "een", "en", "is", "niet", "ik", "je", "jij", "u", "wat", "hoe", "met", "voor", "op", "maar", "ook", "dank", "ja", "nee", "waarom", "dit", "dat", "mijn", "jouw", "van", "zijn", "kun", "kan"},
	"sv": {"och", "är", "inte", "jag", "du", "vad", "hur", "med", "för", "på", "en", "ett", "men", "också", "tack", "ja", "nej", "varför", "det", "den", "min", "din", "av", "kan", "har"},
	"pl": {"i", "jest", "nie", "ja", "ty", "co", "jak", "z", "dla", "na", "ale", "też", "dziękuję", "tak", "dlaczego", "to", "ten", "ta", "mój", "twój", "się", "czy", "możesz", "proszę"},
	"tr": {"ve", "bir", "bu", "ne", "nasıl", "için", "ile", "ama", "çok", "da", "de", "değil", "evet", "hayır", "teşekkürler", "neden", "benim", "senin", "mi", "mı", "var", "yok", "lütfen"},
	"id": {"dan", "yang", "di", "ke", "dari", "ini", "itu", "apa", "bagaimana", "untuk", "dengan", "tidak", "saya", "anda", "kamu", "bisa", "tolong", "terima", "kasih", "ya", "juga", "ada", "mengapa"},
	"vi": {"và", "là", "của", "không", "tôi", "bạn", "gì", "như", "thế", "nào", "cho", "với", "có", "được", "này", "đó", "cảm", "ơn", "vâng", "tại", "sao", "một", "những"},
}

// uniqueWords are function words only one language's list has, which tell
// close languages apart (y in Spanish, e in Italian).
var uniqueWords = func() map[string]bool {
	count := map[string]int{}
	for _, words := range functionWords {
		seen := map[string]bool{}
		for _, w := range words {
			if !seen[w] {
				count[w]++
				seen[w] = true
			}
		}
	}
	out := map[string]bool{}
	for w, n := range count {
		if n == 1 {
			out[w] = true
		}
	}
	return out
}()

var wordSets = func() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for lang, words := range functionWords {
		set := map[string]bool{}
		for _, w := range words {
			set[w] = true
		}
		out[lang] = set
	}
	return out
}()

// letterHints are letters only one or two of these languages use.
var letterHints = map[string]string{
	"es": "ñ¿¡",
	"pt": "ãõ",
	"de": "ßäöü",
	"fr": "œçêëîïû",
	"it": "ì",
	"tr": "ğış",
	"pl": "ąęłńśźż",
	"vi": "ăđơưạảấầẩẫậắằẳẵặẹẻẽếềểễệỉịọỏốồổỗộớờởỡợụủứừửữựỳỵỷỹ",
	"sv": "å",
}

func byWords(text string) (string, bool) {
	lower := strings.ToLower(text)
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\''
	})
	if len(words) < minWords {
		return "", false
	}
	scores := map[string]int{}
	for _, w := range words {
		for lang, set := range wordSets {
			if set[w] {
				scores[lang] += 2
				if uniqueWords[w] {
					scores[lang]++
				}
			}
		}
	}
	for lang, letters := range letterHints {
		if strings.ContainsAny(lower, letters) {
			scores[lang]++
		}
	}
	best, second, bestLang := 0, 0, ""
	for lang, s := range scores {
		switch {
		case s > best:
			best, second, bestLang = s, best, lang
		case s > second:
			second = s
		}
	}
	// Enough evidence, and clearly more than for any other language.
	if best < 4 || best == second || best*5 < second*6 {
		return "", false
	}
	return bestLang, true
}
