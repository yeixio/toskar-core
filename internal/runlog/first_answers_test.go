package runlog

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/store"
)

func TestOriginIsKeptAndNamed(t *testing.T) {
	ctx := WithOrigin(context.Background(), Origin{Client: "phone", Remote: true})
	c := New("r", "", "", "chat")
	c.SetOrigin(OriginFrom(ctx))
	if r := c.Finish(StatusCompleted, nil); r.Client != "phone" || !r.Remote {
		t.Fatalf("run = %+v", r)
	}
	for in, want := range map[string]string{"": "", "watch": "watch", "car": "car", "my-toaster": "other"} {
		if got := ClientName(in); got != want {
			t.Errorf("ClientName(%q) = %q, want %q", in, got, want)
		}
	}
	if (OriginFrom(context.Background()) != Origin{}) {
		t.Fatal("no origin should be the zero Origin")
	}
	if _, asked := OriginOf(context.Background()); asked {
		t.Fatal("work no one asked for in a chat has no origin")
	}
	if _, asked := OriginOf(WithOrigin(context.Background(), Origin{})); !asked {
		t.Fatal("a chat request without a client still has an origin")
	}
}

func run(id, client string, start time.Time, pipeline, firstToken, load float64) Run {
	return Run{ID: id, Client: client, Source: "chat", Status: StatusCompleted, StartedAt: start, PipelineMs: pipeline,
		Models: []ModelUse{{ModelID: "m", Calls: 1, FirstTokenMs: firstToken, LoadMs: load}}}
}

func TestSummarizeFirstAnswers(t *testing.T) {
	now := time.Now()
	runs := []Run{
		run("1", "phone", now, 100, 400, 0),
		run("2", "phone", now, 100, 600, 0),
		run("3", "phone", now, 100, 9000, 8000),
		run("4", "web", now, 50, 300, 0),
		{ID: "5", Client: "phone", Status: StatusFailed, Models: []ModelUse{{FirstTokenMs: 1}}}, // failed
		{ID: "6", Client: "phone", Status: StatusCompleted},                                     // no token
	}
	// Two models: the first word is the earlier first token, and loading adds up.
	two := run("7", "watch", now, 10, 900, 300)
	two.Models = append(two.Models, ModelUse{ModelID: "n", FirstTokenMs: 200, LoadMs: 100})
	two.Remote = true
	runs = append(runs, two)

	got := SummarizeFirstAnswers(now, runs).Groups
	if len(got) != 4 {
		t.Fatalf("groups = %+v", got)
	}
	warm, cold, watch, web := got[0], got[1], got[2], got[3]
	if warm.Client != "phone" || warm.Cold || warm.Runs != 2 || warm.FirstWordMs != 500 || warm.FirstWordP90Ms != 700 || warm.LoadMs != 0 {
		t.Errorf("phone warm = %+v", warm)
	}
	if cold.Client != "phone" || !cold.Cold || cold.Runs != 1 || cold.FirstWordMs != 9100 || cold.LoadMs != 8000 {
		t.Errorf("phone cold = %+v", cold)
	}
	if watch.Client != "watch" || !watch.Cold || watch.FirstWordMs != 210 || watch.LoadMs != 400 || watch.Remote != 1 {
		t.Errorf("watch = %+v", watch)
	}
	if web.Client != "web" || web.Cold || web.FirstWordMs != 350 {
		t.Errorf("web = %+v", web)
	}
}

func TestSinceSkipsAutomationsAndOldRuns(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := NewStore(db.SQL)
	ctx := context.Background()
	now := time.Now().UTC()
	old := run("old", "phone", now.Add(-48*time.Hour), 0, 1, 0)
	auto := run("auto", "", now, 0, 1, 0)
	auto.Source = "automation"
	for _, r := range []Run{run("new", "phone", now, 0, 1, 0), old, auto} {
		if err := s.Save(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Since(ctx, now.Add(-24*time.Hour))
	if err != nil || len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("since = %+v, %v", got, err)
	}
}

func TestLoadTimeIsTheRecentMedianOnThatComputer(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := NewStore(db.SQL)
	ctx := context.Background()
	now := time.Now().UTC()
	for i, load := range []float64{0, 4000, 6000, 20000} {
		r := run(string(rune('a'+i)), "phone", now.Add(time.Duration(i)*time.Second), 0, 1, load)
		r.Models[0].Node = "Office"
		if err := s.Save(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if ms, ok := s.LoadTime(ctx, "m", "Office"); !ok || ms != 6000 {
		t.Fatalf("load time = %v, %v", ms, ok)
	}
	if _, ok := s.LoadTime(ctx, "m", "Den"); ok {
		t.Fatal("another computer's loads count")
	}
	if _, ok := s.LoadTime(ctx, "other", ""); ok {
		t.Fatal("a model that never loaded has a time")
	}
	var none *Store
	if _, ok := none.LoadTime(ctx, "m", ""); ok {
		t.Fatal("nil store")
	}
}
