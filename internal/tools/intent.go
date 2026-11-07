package tools

import (
	"regexp"

	"github.com/yeixio/toskar-core/internal/typo"
)

// liveWebRe matches questions that need current public information. Cues are
// whole words, so "score" does not match "underscore".
var liveWebRe = regexp.MustCompile(`(?i)(\b(weather|forecast|latest version|latest news|who won|last night|right now|today|look up|documentation|summarize (this|the) url|current price|stock price|search the web|search online|news|headlines|this week|this weekend|yesterday|tonight|scores?|exchange rate|price of|cost of|open now|near me|release date|just announced|the latest|what's new|election results|sunrise|sunset|sun rise|sun set|dawn|dusk|first light|golden hour)\b|https?://)`)

// MessageNeedsLiveWeb reports questions that need current public information.
func MessageNeedsLiveWeb(message string) bool {
	return liveWebRe.MatchString(message) || liveWebRe.MatchString(liveWebWords.Fix(message))
}

// The rules' own words, for messages with typos in them (#451): "wether"
// counts as "weather", "recomend" as "recommend".
var (
	liveWebWords = typo.FromPatterns(liveWebRe.String())
	askFindWords = typo.FromPatterns(askFindRe.String())
)

// askFindRe matches a request to find something rather than recall it: a
// recommendation, where to buy, what it costs, reviews, a result or a
// release, or "check" and "find" for me. Small models answer these from
// memory or tell the person to search, so Toskar looks them up first, as
// the iPhone app does (yeixio/toskar-apps mobile/src/intent.ts).
var askFindRe = regexp.MustCompile(`(?i)\b(recommend\w*|suggest (a|an|some)\b.{0,30}\b(to buy|for sale|product|brand|model|bike|bicycle|machine|phone|laptop|car|tool|app)s?|best \w+(?: \w+){0,5} (for|under|to buy)|which \w+(?: \w+){0,5} should i (buy|get|choose)|where (can|could|do|should) i (buy|find|get|order|rent)|for sale|how much (is|are|does|do|did)|reviews?|check (for me|it|that|this|them|again)|can you (check|find|look)|find (me|out)|winner|won the|new release|released?|links? to)\b`)

// MessageAsksToFind reports requests to find something on the web.
func MessageAsksToFind(message string) bool {
	return askFindRe.MatchString(message) || askFindRe.MatchString(askFindWords.Fix(message))
}

// askPlacesRe matches a request for a kind of place: "Find a cafe in
// downtown Juneau", "best pizza places near the harbor". Models that don't
// look it up themselves, such as Qwen 3.5, make up names and addresses, so
// Toskar looks it up first (#1126). It needs a word that asks, so "a story
// set in a cafe" isn't looked up.
var askPlacesRe = regexp.MustCompile(`(?i)\b(find|where('s| is| are| can i)|best|recommend\w*|suggest|looking for|is there|are there|list)\b.{0,40}\b(caf[eé]s?|coffee (shops?|houses?)|restaurants?|diners?|pizza( places)?|bars?|pubs?|breweries|brewery|bakeries|bakery|pharmac(y|ies)|gas stations?|hotels?|motels?|grocery( stores?)?|groceries|supermarkets?|museums?|gyms?|hospitals?|clinics?|urgent care|vets?|libraries|library|post offices?|laundromats?|campgrounds?|trailheads?)\b`)

var askPlacesWords = typo.FromPatterns(askPlacesRe.String())

// MessageAsksForPlaces reports a request for places of a kind, to look up.
func MessageAsksForPlaces(message string) bool {
	return askPlacesRe.MatchString(message) || askPlacesRe.MatchString(askPlacesWords.Fix(message))
}
