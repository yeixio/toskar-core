package huginn

import (
	"regexp"
	"strconv"
	"strings"
)

// swarmRe asks for a swarm in so many words (#1135): "use a swarm", "use 3
// agents", "using four agents". English only, as goOnRe is.
var swarmRe = regexp.MustCompile(`(?i)\b(?:use|using|with)\s+(?:a\s+swarm\b|(\d+|two|three|four|five|six|seven|eight)\s+agents\b)`)

var swarmWords = map[string]int{"two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8}

// AsksForSwarm reports a message that asks for a swarm (#1135), and how many
// agents it named, or 0.
func AsksForSwarm(message string) (agents int, ok bool) {
	loc := swarmRe.FindStringSubmatchIndex(message)
	if loc == nil {
		return 0, false
	}
	// "Use a swarm of bees in the poem" is about bees.
	if rest := strings.ToLower(strings.TrimSpace(message[loc[1]:])); strings.HasPrefix(rest, "of ") {
		return 0, false
	}
	m := []string{message[loc[0]:loc[1]], ""}
	if loc[2] >= 0 {
		m[1] = message[loc[2]:loc[3]]
	}
	if m[1] == "" {
		return 0, true
	}
	if n, err := strconv.Atoi(m[1]); err == nil {
		return n, true
	}
	return swarmWords[strings.ToLower(m[1])], true
}
