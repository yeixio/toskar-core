package tools

import "regexp"

// liveWebRe matches questions that need current public information. Cues are
// whole words, so "score" does not match "underscore".
var liveWebRe = regexp.MustCompile(`(?i)(\b(weather|forecast|latest version|latest news|who won|last night|right now|today|look up|documentation|summarize (this|the) url|current price|stock price|search the web|search online|news|headlines|this week|this weekend|yesterday|tonight|scores?|exchange rate|price of|cost of|open now|near me|release date|just announced|the latest|what's new|election results)\b|https?://)`)

// MessageNeedsLiveWeb reports questions that need current public information.
func MessageNeedsLiveWeb(message string) bool {
	return liveWebRe.MatchString(message)
}
