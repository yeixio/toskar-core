package connectors

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/tools"
)

// Flights looks up a flight's status with AeroDataBox (#710): gate,
// terminal, scheduled and expected times, and whether it's delayed. Web
// search rarely has live gates. The person brings their own key, from
// API.market or RapidAPI, both of which have a free tier.
type Flights struct {
	// Now is today's date for a look-up without one; nil is the clock.
	Now func() time.Time
}

func (Flights) ID() string   { return "flights" }
func (Flights) Name() string { return "Flight status (AeroDataBox)" }
func (Flights) Description() string {
	return "Look up a flight's gate, terminal, times, and delays by its number, such as AA3595."
}
func (Flights) Scopes() string {
	return "Sign up for AeroDataBox on API.market (api.market/store/aedbx/aerodatabox) or RapidAPI and copy your API key; the free tier is enough for a few look-ups a day. " +
		"Each question about a flight is one look-up, sent with the flight's number and date."
}

func (Flights) Fields() []Field {
	return []Field{
		{Key: "key", Label: "API key", Secret: true},
		{Key: "api_url", Label: "API address", Optional: true, Placeholder: flightsAPIMarket,
			Help: "Only for a RapidAPI key: https://aerodatabox.p.rapidapi.com"},
	}
}

const flightsAPIMarket = "https://prod.api.market/api/v1/aedbx/aerodatabox"

func flightsBase(cred Credential) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(cred["api_url"]), "/")
	if base == "" {
		return flightsAPIMarket, nil
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("the API address must start with https://, such as %s", flightsAPIMarket)
	}
	return base, nil
}

// flightsHeader is the key in the header its gateway reads.
func flightsHeader(base string, cred Credential) map[string]string {
	if u, err := url.Parse(base); err == nil && strings.HasSuffix(u.Hostname(), "rapidapi.com") {
		return map[string]string{"X-RapidAPI-Key": cred["key"], "X-RapidAPI-Host": u.Hostname()}
	}
	return map[string]string{"x-api-market-key": cred["key"]}
}

// flightRe is a flight number: an airline code of two or three letters or
// digits and one to four digits, with or without a space, such as AA3595,
// UA 12, or 9W123.
var flightRe = regexp.MustCompile(`(?i)^([a-z]{2,3}|[a-z][0-9]|[0-9][a-z])\s?([0-9]{1,4}[a-z]?)$`)

// FlightNumber reads "aa 3595" as AA3595, or "" when it isn't a flight number.
func FlightNumber(s string) string {
	m := flightRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return ""
	}
	return strings.ToUpper(m[1] + m[2])
}

type flightTime struct {
	UTC   string `json:"utc"`
	Local string `json:"local"`
}

type flightMove struct {
	Airport struct {
		IATA             string `json:"iata"`
		ICAO             string `json:"icao"`
		Name             string `json:"name"`
		ShortName        string `json:"shortName"`
		MunicipalityName string `json:"municipalityName"`
	} `json:"airport"`
	ScheduledTime *flightTime `json:"scheduledTime"`
	RevisedTime   *flightTime `json:"revisedTime"`
	PredictedTime *flightTime `json:"predictedTime"`
	RunwayTime    *flightTime `json:"runwayTime"`
	Terminal      string      `json:"terminal"`
	CheckInDesk   string      `json:"checkInDesk"`
	Gate          string      `json:"gate"`
	BaggageBelt   string      `json:"baggageBelt"`
}

type flight struct {
	Number         string     `json:"number"`
	Status         string     `json:"status"`
	CodeshareState string     `json:"codeshareStatus"`
	LastUpdatedUTC string     `json:"lastUpdatedUtc"`
	Departure      flightMove `json:"departure"`
	Arrival        flightMove `json:"arrival"`
	Airline        struct {
		Name string `json:"name"`
		IATA string `json:"iata"`
	} `json:"airline"`
	Aircraft struct {
		Model string `json:"model"`
	} `json:"aircraft"`
}

// flightStatus says a status as people do: EnRoute is "en route".
var flightStatus = map[string]string{
	"Unknown": "unknown", "Expected": "expected (on schedule)", "EnRoute": "en route", "CheckIn": "check-in open", "Boarding": "boarding",
	"GateClosed": "gate closed", "Departed": "departed", "Delayed": "delayed", "Approaching": "approaching", "Arrived": "arrived",
	"Canceled": "canceled", "Diverted": "diverted", "CanceledUncertain": "possibly canceled",
}

// localClock is a local time as the airport shows it: "2026-11-18 07:05-06:00"
// gives "07:05".
func localClock(t *flightTime) string {
	if t == nil || len(t.Local) < 16 {
		return ""
	}
	return t.Local[11:16]
}

// parseUTC reads AeroDataBox's "2026-11-18 13:05Z", or RFC 3339.
func parseUTC(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04Z", time.RFC3339, "2006-01-02T15:04Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// minutesLate compares the expected time with the schedule.
func minutesLate(m flightMove) int {
	if m.ScheduledTime == nil {
		return 0
	}
	expected := m.RevisedTime
	if expected == nil {
		expected = m.PredictedTime
	}
	if expected == nil {
		return 0
	}
	s, ok1 := parseUTC(m.ScheduledTime.UTC)
	e, ok2 := parseUTC(expected.UTC)
	if !ok1 || !ok2 {
		return 0
	}
	return int(e.Sub(s).Minutes())
}

func (m flightMove) view(arrival bool) map[string]any {
	out := map[string]any{}
	name := m.Airport.Name
	if m.Airport.MunicipalityName != "" && !strings.Contains(name, m.Airport.MunicipalityName) {
		name = m.Airport.MunicipalityName + " " + name
	}
	if m.Airport.IATA != "" {
		name += " (" + m.Airport.IATA + ")"
	}
	out["airport"] = strings.TrimSpace(name)
	for k, v := range map[string]string{"terminal": m.Terminal, "gate": m.Gate, "check_in_desk": m.CheckInDesk, "baggage_belt": m.BaggageBelt,
		"scheduled": localClock(m.ScheduledTime), "expected": localClock(m.RevisedTime)} {
		if v != "" {
			out[k] = v
		}
	}
	if _, ok := out["expected"]; !ok {
		if p := localClock(m.PredictedTime); p != "" {
			out["expected"] = p
		}
	}
	if late := minutesLate(m); late >= 5 {
		out["minutes_late"] = late
	}
	if m.ScheduledTime != nil && len(m.ScheduledTime.Local) >= 10 {
		out["date"] = m.ScheduledTime.Local[:10]
	}
	if !arrival && m.Gate == "" {
		out["gate"] = "not announced yet"
	}
	return out
}

func (f flight) view() map[string]any {
	status := flightStatus[f.Status]
	if status == "" {
		status = strings.ToLower(f.Status)
	}
	out := map[string]any{
		"flight":    f.Number,
		"status":    status,
		"departure": f.Departure.view(false),
		"arrival":   f.Arrival.view(true),
	}
	if f.Airline.Name != "" {
		out["airline"] = f.Airline.Name
	}
	if f.Aircraft.Model != "" {
		out["aircraft"] = f.Aircraft.Model
	}
	if f.LastUpdatedUTC != "" {
		out["updated_utc"] = f.LastUpdatedUTC
	}
	return out
}

func (fl Flights) today() time.Time {
	if fl.Now != nil {
		return fl.Now()
	}
	return time.Now()
}

// lookUp reads one flight on one local date.
func flightsLookUp(ctx context.Context, c *http.Client, cred Credential, number, date string) ([]flight, error) {
	base, err := flightsBase(cred)
	if err != nil {
		return nil, err
	}
	var list []flight
	u := fmt.Sprintf("%s/flights/number/%s/%s", base, url.PathEscape(number), url.PathEscape(date))
	if err := call(ctx, c, http.MethodGet, u, flightsHeader(base, cred), nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (fl Flights) Check(ctx context.Context, c *http.Client, cred Credential) (string, error) {
	if strings.TrimSpace(cred["key"]) == "" {
		return "", fmt.Errorf("the API key is required")
	}
	// One look-up shows the key works; a flight that doesn't fly today is
	// an empty answer, not an error.
	if _, err := flightsLookUp(ctx, c, cred, "DL1", fl.today().Format("2006-01-02")); err != nil {
		return "", err
	}
	base, _ := flightsBase(cred)
	u, _ := url.Parse(base)
	return u.Hostname(), nil
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (fl Flights) Tools() []Tool {
	return []Tool{{
		Def: tools.Definition{ID: "flights.status", Name: "Flight Status", Capability: "flights", Risk: tools.RiskRead, DefaultPolicy: tools.PolicyAllow,
			Description: "A flight's live status from AeroDataBox: departure and arrival airports, terminal, gate, check-in desk, baggage belt, scheduled and expected local times, minutes late, and status (such as boarding, delayed, or canceled). " +
				"\"flight\" is its number with the airline's code, such as AA3595 for American Airlines flight 3595 or UA12; \"date\" is the local date it departs, YYYY-MM-DD, today when left out. " +
				"Use it, not web search, for gates, delays, and departure or arrival times, and say when the data was last updated.",
			Schema: `{"flight":"string","date":"string"}`},
		Run: func(ctx context.Context, c *http.Client, cred Credential, args map[string]any) (map[string]any, error) {
			number := FlightNumber(str(args, "flight"))
			if number == "" {
				return nil, fmt.Errorf("flight must be a flight number with the airline's code, such as AA3595")
			}
			date := str(args, "date")
			if date == "" {
				date = fl.today().Format("2006-01-02")
			}
			if !dateRe.MatchString(date) {
				return nil, fmt.Errorf("date must be YYYY-MM-DD")
			}
			list, err := flightsLookUp(ctx, c, cred, number, date)
			if err != nil {
				return nil, err
			}
			flights := make([]any, 0, len(list))
			for _, f := range list {
				flights = append(flights, f.view())
			}
			out := map[string]any{"flights": flights, "source": "AeroDataBox", "checked_at": time.Now().UTC().Format(time.RFC3339)}
			if len(flights) == 0 {
				out["note"] = fmt.Sprintf("No flight %s found on %s. It may not fly that day; ask for the date.", number, date)
			}
			return out, nil
		},
	}}
}
