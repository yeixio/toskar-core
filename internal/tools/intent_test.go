package tools

import "testing"

// A request for places of a kind is looked up first, whatever the model
// would do (#1126); a place named in passing isn't.
func TestMessageAsksForPlaces(t *testing.T) {
	for _, msg := range []string{
		"Find a cafe in downtown Juneau",
		"Where's a good pharmacy around here?",
		"best pizza places near the harbor",
		"Can you recommend a restaurant in Sitka?",
		"Are there any campgrounds near Mendenhall Glacier?",
		"I'm looking for a laundromat in Juneau",
		"find a resturant in juneau",
		// #1381: a kind of place with where it is, and no asking word.
		"What restaurants are in town?",
		"What are some good restaurants in town?",
		"Any good places to eat in town?",
		"Where should we go for dinner?",
		"Tell me about restaurants in Juneau",
		"Give me 5 restaurants in town",
		"local coffee shops",
	} {
		if !MessageAsksForPlaces(msg) {
			t.Errorf("%q: not looked up", msg)
		}
	}
	for _, msg := range []string{
		"Write a short story set in a cafe",
		"What does a pharmacy technician do?",
		"How do I open a restaurant?",
		"Translate 'the library is closed' into Spanish",
		"Find the bug in this function",
		"How do I color the bars in a matplotlib chart?",
		"Why do restaurants fail?",
		"Write a poem about a bakery",
	} {
		if MessageAsksForPlaces(msg) {
			t.Errorf("%q: looked up", msg)
		}
	}
}
