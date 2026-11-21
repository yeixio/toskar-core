package voicebench

import (
	"encoding/base64"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/yeixio/toskar-core/internal/replylang"
	"github.com/yeixio/toskar-core/internal/voice"
)

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func decodeAudio(b64 string) []int16 {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil
	}
	return voice.DecodePCM16(b)
}

// words is text as compared for word error rate: lower case, without
// punctuation or accents, so "¿Qué tiempo hará?" and "que tiempo hara"
// are the same words.
func words(s string) []string {
	s = norm.NFD.String(strings.ToLower(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '\'' || r == '’':
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Fields(b.String())
}

// wer is the word error rate of got against want: substitutions,
// deletions, and insertions over want's words.
func wer(want, got string) float64 {
	w, g := words(want), words(got)
	if len(w) == 0 {
		return 0
	}
	return float64(editDistance(w, g)) / float64(len(w))
}

func editDistance(a, b []string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			sub := prev[j-1]
			if a[i-1] != b[j-1] {
				sub++
			}
			cur[j] = min(sub, prev[j]+1, cur[j-1]+1)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// replyLanguage is the language an answer is in, as Toskar tells it, or ""
// when it's too short to tell.
func replyLanguage(text string) string {
	if tag, ok := replylang.Detect(text); ok {
		return tag
	}
	return ""
}
