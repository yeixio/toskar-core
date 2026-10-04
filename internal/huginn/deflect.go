package huginn

import "regexp"

// deflectRe matches answers that send the person off to find the answer
// themselves, or pretend to browse: "search for it on Google", "check
// websites like", "I don't have real-time access", "[Searching…]". The
// quality set's deflection pattern (tests/quality/cases.json) and the
// iPhone app's check match the same answers.
var deflectRe = regexp.MustCompile(`(?i)(search (for|online|the web|on google)|search engines?|(on|use|using) (google|bing|safari)\b|(check|visit|try|browse) (out )?(websites?|online|sites|retailers|the web|stores)|websites? (like|such as)|\bI (don't|do not|can't|cannot|am unable to|am not able to) (have )?(real-time|access|browse|search|look|check|provide (links|real-time|current))|real-time (access|data|information)|\[search|as an AI\b|I'm (just )?an? (text-based|language model|AI)|\b(the user|you) (needs?|should|can|could|may want|might want|will need|would need|have)( to)? (check|search|look|visit|contact|consult|try|google))`)

// Deflects reports an answer that tells the person to find it themselves
// instead of answering (§21).
func Deflects(answer string) bool {
	return deflectRe.MatchString(answer)
}
