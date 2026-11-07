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
	} {
		if MessageAsksForPlaces(msg) {
			t.Errorf("%q: looked up", msg)
		}
	}
}
