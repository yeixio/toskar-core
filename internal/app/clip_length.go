package app

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/yeixio/toskar-core/internal/tools"
)

// Numbers as words, for lengths such as "one second" or "zwei Sekunden".
// Articles aren't among them: "a second video", "une seconde vidéo", and
// "un segundo video" mean another one, not one second long. An article
// counts only before a minute (aMinuteRe). Korean's number words are
// everyday words too (이, 오), so Korean lengths are read from digits.
var lengthWords = map[string]float64{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
	"ein": 1, "eine": 1, "einen": 1, "einer": 1, "zwei": 2, "drei": 3, "vier": 4, "fünf": 5, "sechs": 6, "sieben": 7, "acht": 8, "neun": 9, "zehn": 10,
	"dos": 2, "tres": 3, "cuatro": 4, "cinco": 5, "seis": 6, "siete": 7, "ocho": 8, "nueve": 9, "diez": 10,
	"deux": 2, "trois": 3, "quatre": 4, "cinq": 5, "sept": 7, "huit": 8, "neuf": 9, "dix": 10,
	"due": 2, "tre": 3, "quattro": 4, "sei": 6, "sette": 7, "otto": 8, "nove": 9, "dieci": 10,
	"dois": 2, "duas": 2, "três": 3, "quatro": 4, "oito": 8, "dez": 10,
}

// hanNumbers are Chinese and Japanese numerals, up to ten.
var hanNumbers = map[string]float64{"一": 1, "二": 2, "两": 2, "兩": 2, "三": 3, "四": 4, "五": 5, "六": 6, "七": 7, "八": 8, "九": 9, "十": 10}

// latinLengthRe finds a length in a language written with spaces: a number,
// as digits or a word standing alone, then seconds or minutes, then the
// end of the word ("2 sisters" isn't two seconds).
var latinLengthRe = func() *regexp.Regexp {
	words := make([]string, 0, len(lengthWords))
	for w := range lengthWords {
		words = append(words, regexp.QuoteMeta(w))
	}
	// Longer words first, so "einen" isn't read as "ein".
	sort.Slice(words, func(i, j int) bool { return len(words[i]) > len(words[j]) })
	return regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?|(?:^|[^\p{L}])(?:` + strings.Join(words, "|") + `))\s*-?\s*` +
		`(s|secs?|seconds?|sekunden?|segundos?|secondes?|secondi|secondo|mins?|minutes?|minuten?|minutos?|minuti|minuto)(?:[^\p{L}]|$)`)
}()

// cjkLengthRe finds a length in Chinese, Japanese, or Korean, where a
// particle follows the unit with no space (3秒で, 五秒的, 3초로). A bare 分
// takes digits only: 十分 is "enough" in Japanese.
var cjkLengthRe = regexp.MustCompile(`(\d+(?:\.\d+)?|[一二两兩三四五六七八九十]+)(秒钟|秒鐘|秒|分钟|分鐘)|(\d+(?:\.\d+)?)(分|초|분)`)

// halfMinuteRe is half a minute, and aMinuteRe a minute, which have no
// number in them.
var (
	halfMinuteRe = regexp.MustCompile(`(?i)(half a minute|half-minute|halbe minute|medio minuto|demi-minute|mezzo minuto|meio minuto|半分钟|半分鐘)`)
	aMinuteRe    = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(a|an|un|une|uno|una|um|uma) (minute|minuto|minuta)(?:[^\p{L}]|$)`)
)

// lengthIn reads a number and its unit, in seconds; 0 when it isn't one.
func lengthIn(number, unit string) float64 {
	number = strings.ToLower(strings.TrimLeftFunc(number, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
	n, err := strconv.ParseFloat(strings.Replace(number, ",", ".", 1), 64)
	if err != nil {
		var ok bool
		if n, ok = lengthWords[number]; !ok {
			if n, ok = hanNumbers[number]; !ok {
				return 0
			}
		}
	}
	unit = strings.ToLower(unit)
	// "1990s" is a decade, not a length.
	if unit == "s" && n >= 100 {
		return 0
	}
	if strings.HasPrefix(unit, "m") || strings.HasPrefix(unit, "分") || unit == "분" {
		n *= 60
	}
	return n
}

// askedSeconds is the clip length the person's message asks for, in
// seconds: "just one second long", "a 3s clip", "half a minute", "zwei
// Sekunden". It's 0 when the message names no length, or names more than
// one.
func askedSeconds(message string) float64 {
	found := map[float64]bool{}
	// Half a minute has "a minute" in it, so it's read and taken out first.
	if halfMinuteRe.MatchString(message) {
		found[30] = true
		message = halfMinuteRe.ReplaceAllString(message, " ")
	}
	if aMinuteRe.MatchString(message) {
		found[60] = true
	}
	for _, m := range latinLengthRe.FindAllStringSubmatch(message, -1) {
		if n := lengthIn(m[1], m[2]); n > 0 {
			found[n] = true
		}
	}
	for _, m := range cjkLengthRe.FindAllStringSubmatch(message, -1) {
		number, unit := m[1], m[2]
		if number == "" {
			number, unit = m[3], m[4]
		}
		if n := lengthIn(number, unit); n > 0 {
			found[n] = true
		}
	}
	if len(found) != 1 {
		return 0
	}
	for n := range found {
		return n
	}
	return 0
}

// withAskedLength gives video.generate the length the person's message
// asks for (#789). A small model often leaves seconds out, or passes the
// default, so "just one second long" became two seconds. The person's own
// words win over the model's. A length past the most a clip can be is
// still passed, so the clip is made at the most and the answer says so.
func withAskedLength(message, toolID string, args map[string]any) map[string]any {
	if tools.Canonical(toolID) != "video.generate" {
		return args
	}
	asked := askedSeconds(message)
	if asked <= 0 {
		return args
	}
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	out["seconds"] = asked
	return out
}
