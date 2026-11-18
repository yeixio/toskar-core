package connectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/tools"
)

// An AeroDataBox answer for American Airlines 3595 from Chicago O'Hare, 20
// minutes late, with its gate.
const aa3595 = `[{"greatCircleDistance":{"km":1000},"departure":{"airport":{"icao":"KORD","iata":"ORD","name":"Chicago O'Hare","shortName":"O'Hare","municipalityName":"Chicago","countryCode":"US","timeZone":"America/Chicago"},
"scheduledTime":{"utc":"2026-11-18 13:05Z","local":"2026-11-18 07:05-06:00"},"revisedTime":{"utc":"2026-11-18 13:25Z","local":"2026-11-18 07:25-06:00"},
"terminal":"3","gate":"H14","checkInDesk":null,"quality":["Basic","Live"]},
"arrival":{"airport":{"iata":"DCA","name":"Washington Ronald Reagan National","municipalityName":"Washington"},"scheduledTime":{"utc":"2026-11-18 15:10Z","local":"2026-11-18 10:10-05:00"},"terminal":"2","baggageBelt":"7","quality":["Basic"]},
"lastUpdatedUtc":"2026-11-18 12:40Z","number":"AA 3595","callSign":null,"status":"Delayed","codeshareStatus":"IsOperator","isCargo":false,
"aircraft":{"model":"Embraer 175"},"airline":{"name":"American Airlines","iata":"AA","icao":"AAL"}}]`

func fakeAeroDataBox(t *testing.T, key string, rapid bool) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("x-api-market-key")
		if rapid {
			got = r.Header.Get("X-RapidAPI-Key")
			if r.Header.Get("X-RapidAPI-Host") == "" {
				t.Error("no X-RapidAPI-Host")
			}
		}
		if got != key {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Invalid API key"}`))
			return
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/flights/number/AA3595/2026-11-18":
			_, _ = w.Write([]byte(aa3595))
		default:
			// AeroDataBox answers 204 for a flight that doesn't fly that day.
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

func TestFlightNumber(t *testing.T) {
	for in, want := range map[string]string{"AA3595": "AA3595", "aa 3595": "AA3595", "UA12": "UA12", "9W123": "9W123", "B6 615": "B6615", "DAL1": "DAL1", "flight": "", "AA": "", "3595": "", "AA35951": ""} {
		if got := FlightNumber(in); got != want {
			t.Errorf("FlightNumber(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFlights(t *testing.T) {
	srv, paths := fakeAeroDataBox(t, "adb-key", false)
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(); tools.SetConnected("flights", nil) })
	reg := &memRegistry{tools: map[string]tools.Tool{}}
	today := func() time.Time { return time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC) }
	m := NewManager(db.SQL, &memSecrets{m: map[string]string{}}, reg, Flights{Now: today})
	ctx := context.Background()
	if _, err := m.Connect(ctx, "flights", map[string]string{"key": "wrong", "api_url": srv.URL}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("wrong key: %v", err)
	}
	if _, err := m.Connect(ctx, "flights", map[string]string{"key": "adb-key", "api_url": srv.URL}); err != nil {
		t.Fatal(err)
	}
	if (*paths)[0] != "/flights/number/DL1/2026-11-18" {
		t.Fatalf("check = %v", *paths)
	}
	def, ok := tools.Lookup("flights.status")
	if !ok || def.Risk != tools.RiskRead || def.Source != "connector:flights" {
		t.Fatalf("def = %+v", def)
	}
	res, err := reg.tools["flights.status"].Execute(ctx, map[string]any{"flight": "aa 3595"})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := res["flights"].([]any)
	if len(list) != 1 {
		t.Fatalf("res = %+v", res)
	}
	f := list[0].(map[string]any)
	dep := f["departure"].(map[string]any)
	arr := f["arrival"].(map[string]any)
	if f["status"] != "delayed" || f["airline"] != "American Airlines" || dep["gate"] != "H14" || dep["terminal"] != "3" ||
		dep["scheduled"] != "07:05" || dep["expected"] != "07:25" || dep["minutes_late"] != 20 || dep["airport"] != "Chicago O'Hare (ORD)" {
		t.Fatalf("flight = %+v", f)
	}
	if arr["baggage_belt"] != "7" || arr["airport"] != "Washington Ronald Reagan National (DCA)" || arr["scheduled"] != "10:10" {
		t.Fatalf("arrival = %+v", arr)
	}
	// Not that day: an empty answer that says so.
	res, err = reg.tools["flights.status"].Execute(ctx, map[string]any{"flight": "AA3595", "date": "2026-11-19"})
	if err != nil || !strings.Contains(res["note"].(string), "No flight AA3595") {
		t.Fatalf("other day = %+v, %v", res, err)
	}
	for _, bad := range []map[string]any{{"flight": "American"}, {"flight": "AA3595", "date": "tomorrow"}} {
		if _, err := reg.tools["flights.status"].Execute(ctx, bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestFlightsRapidAPIHeaders(t *testing.T) {
	srv, _ := fakeAeroDataBox(t, "rapid-key", true)
	if h := flightsHeader("https://aerodatabox.p.rapidapi.com", Credential{"key": "k"}); h["X-RapidAPI-Host"] != "aerodatabox.p.rapidapi.com" || h["X-RapidAPI-Key"] != "k" {
		t.Fatalf("headers = %v", h)
	}
	// The fake isn't on rapidapi.com, so call it with RapidAPI's headers directly.
	list, err := func() ([]flight, error) {
		var out []flight
		err := call(context.Background(), http.DefaultClient, http.MethodGet, srv.URL+"/flights/number/AA3595/2026-11-18",
			map[string]string{"X-RapidAPI-Key": "rapid-key", "X-RapidAPI-Host": "aerodatabox.p.rapidapi.com"}, nil, &out)
		return out, err
	}()
	if err != nil || len(list) != 1 || list[0].Departure.Gate != "H14" {
		t.Fatalf("rapid = %+v, %v", list, err)
	}
	if h := flightsHeader(flightsAPIMarket, Credential{"key": "k"}); h["x-api-market-key"] != "k" {
		t.Fatalf("api.market headers = %v", h)
	}
}
