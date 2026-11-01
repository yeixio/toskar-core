package tools

import (
	"regexp"
	"strings"
)

// LiveText turns a model's output into text to show while it's written
// (#1006). What it shows is ParseModelOutput's answer so far, less anything
// that could still become a protocol span, so a wrapper is never shown. The
// whole output decides the answer: text shown here can still be taken back.
type LiveText struct {
	raw     strings.Builder
	shown   string
	stopped bool
}

// Feed takes the next piece of output and returns the text newly safe to
// show. Parsing can change its mind about earlier text, and then nothing more
// is shown: what comes out of the whole output replaces it.
func (l *LiveText) Feed(piece string) string {
	if l.stopped || piece == "" {
		return ""
	}
	l.raw.WriteString(piece)
	raw := l.raw.String()
	visible := ParseModelOutput(raw[:safeEnd(raw)]).Text
	if loc := liveFence.FindStringIndex(visible); loc != nil {
		visible = strings.TrimSpace(visible[:loc[0]])
	}
	// Less than was shown just means more is held back for now.
	if strings.HasPrefix(l.shown, visible) {
		return ""
	}
	if !strings.HasPrefix(visible, l.shown) {
		l.stopped = true
		return ""
	}
	delta := visible[len(l.shown):]
	l.shown = visible
	return delta
}

// Shown is the text shown so far.
func (l *LiveText) Shown() string { return l.shown }

var (
	// A fence that may still turn out empty, with a call taken out of it.
	// The parser removes an empty one from the answer.
	liveFence = regexp.MustCompile("(?i)(?:^|\n)[ \t]*`{1,3}(?:j|js|jso|json)?\\s*`{0,3}$")
	// A tool's name written out, which may be a call in prose (looseCallRe).
	liveToolName = regexp.MustCompile(`[a-z]+\.[a-z_]+\s*$`)
)

// liveMarkers start a protocol span. functionCall is treated apart, because
// its span needs the parenthesis after it.
var liveMarkers = []string{"<tool_call>", "<tool_result>", "<|"}

const liveFunctionCall = "function_call"

// safeEnd is how much of raw can't turn into more of a protocol span: it
// stops at the first object that isn't closed, since any object can turn out
// to hold a call, and at the start of a marker still being written.
func safeEnd(raw string) int {
	end := len(raw)
	for i := 0; i < len(raw); i++ {
		if raw[i] != '{' {
			continue
		}
		close, ok := jsonObjectEnd(raw, i)
		if !ok {
			end = i
			break
		}
		i = close - 1
	}
	lower := strings.ToLower(raw[:end])
	for _, marker := range liveMarkers {
		if i := partialAtEnd(lower, marker); i >= 0 {
			end = min(end, i)
		}
	}
	if i := strings.LastIndex(lower, "<|"); i >= 0 && !strings.Contains(lower[i:], "|>") {
		end = min(end, i)
	}
	if i := partialAtEnd(lower, liveFunctionCall); i >= 0 {
		end = min(end, i)
	} else if t := strings.TrimRight(lower, " \t\n"); strings.HasSuffix(t, liveFunctionCall) {
		end = min(end, len(t)-len(liveFunctionCall))
	}
	if loc := liveToolName.FindStringIndex(raw[:end]); loc != nil {
		end = loc[0]
	}
	return end
}

// partialAtEnd is where text ends with the start of marker, or -1. A marker
// written out in full isn't partial: the parser reads it.
func partialAtEnd(text, marker string) int {
	for n := min(len(marker)-1, len(text)); n > 0; n-- {
		if strings.HasSuffix(text, marker[:n]) {
			return len(text) - n
		}
	}
	return -1
}
