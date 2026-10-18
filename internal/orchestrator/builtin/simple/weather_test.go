package simple

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A question about a month, a season, or the usual weather is about the
// climate; one about now is not (#280).
func TestClimateQuestion(t *testing.T) {
	for msg, want := range map[string]bool{
		"What's the weather gonna be like in June, Alaska around lunch time?": true,
		"What's the weather like in Juneau in June?":                          true,
		"What's the usual weather in Lisbon in the fall?":                     true,
		"Average temperature in Tokyo in winter":                              true,
		"What's the weather in Juneau today?":                                 false,
		"Juneau weather forecast for today":                                   false,
		"Will it rain tomorrow in Seattle? Check the weather":                 false,
		"What's the weather in June right now?":                               false,
		"Is June a good month to visit?":                                      false,
		"May I have the weather for Paris?":                                   false,
	} {
		if got := climateQuestion(msg); got != want {
			t.Errorf("climateQuestion(%q) = %v, want %v", msg, got, want)
		}
	}
}

// A climate question doesn't read today's wttr.in reading first, and puts
// forecast pages after the others; a question about now does the opposite.
func TestClimateQuestionSkipsTodaysReading(t *testing.T) {
	results := map[string]any{"results": []any{
		map[string]any{"url": "https://forecast.weather.gov/MapClick.php?lat=58.30"},
		map[string]any{"url": "https://weather.com/weather/monthly/l/Juneau"},
	}}
	june := livePageURLs("What's the weather like in Juneau in June?", map[string]any{"query": "Juneau weather June"}, results)
	if !slices.Equal(june, []string{"https://weather.com/weather/monthly/l/Juneau", "https://forecast.weather.gov/MapClick.php?lat=58.30"}) {
		t.Fatalf("climate pages = %v", june)
	}
	today := livePageURLs("What's the weather in Juneau today?", map[string]any{"query": "Juneau weather"}, results)
	if !strings.HasPrefix(today[0], "https://wttr.in/juneau?") || today[1] != "https://forecast.weather.gov/MapClick.php?lat=58.30" {
		t.Fatalf("pages for now = %v", today)
	}
	if got := weatherPlace("what's the weather going to be like outside in Juneau"); got != "juneau" {
		t.Fatalf("place = %q", got)
	}
}

// Text a model writes before a tool call isn't part of the answer (#280).
func TestTextBeforeAToolCallIsNotShown(t *testing.T) {
	env := &scriptedEnv{replies: []string{
		"Let me find that information for you.\n" + `{"tool_call":{"id":"internet.search","args":{"query":"Juneau weather"}}}`,
		"It is 48 F and cloudy in Juneau.",
	}}
	events, err := New().Run(context.Background(), contracts.Task{Prompt: "weather"}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{{ToolID: "internet.search", Policy: "ask"}},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for evt := range events {
		if evt.Type == "agent.message" {
			text += evt.Content
		}
	}
	if env.tools != 1 || text != "It is 48 F and cloudy in Juneau." {
		t.Fatalf("tools=%d text=%q", env.tools, text)
	}
}

// A sunrise or sunset question reads the times from the weather service, in
// the place's local time, first (toskar-apps#255).
func TestSunTimesComeFromTheWeatherService(t *testing.T) {
	results := map[string]any{"results": []any{map[string]any{"url": "https://example.com/sunrise-chicago"}}}
	urls := livePageURLs("What time is sunrise in chicago this morning?", map[string]any{"query": "sunrise chicago"}, results)
	if len(urls) != 2 || !strings.HasPrefix(urls[0], "https://wttr.in/chicago?format=") || !strings.Contains(urls[0], "%25S") {
		t.Fatalf("sunrise urls = %v", urls)
	}
	cases := map[string]string{
		"What time is sunrise in chicago this morning?": "chicago",
		"When does the sun set in Juneau tonight?":      "juneau",
		"sunset times for San Francisco":                "san francisco",
		"what time is dawn":                             "",
	}
	for in, want := range cases {
		if got := sunPlace(in); got != want {
			t.Errorf("sunPlace(%q) = %q, want %q", in, got, want)
		}
	}
	// Not a sun question: the weather reading as before.
	if urls := livePageURLs("What's the weather in Juneau today?", map[string]any{"query": "Juneau weather"}, results); strings.Contains(urls[0], "%25S") {
		t.Fatalf("weather got sun times: %v", urls)
	}
}
