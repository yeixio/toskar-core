package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// resultsFixture makes automations for two people, with runs.
type resultsFixture struct {
	repo     *repositories.AutomationRepo
	sam, ada context.Context
}

func newResultsFixture(t *testing.T) resultsFixture {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	base := locale.WithTimeZone(context.Background(), "America/Juneau")
	return resultsFixture{
		repo: repositories.NewAutomationRepo(db.SQL),
		sam:  auth.AsPerson(base, auth.Person{ID: "sam", Role: auth.RoleMember}),
		ada:  auth.AsPerson(base, auth.Person{ID: "ada", Role: auth.RoleMember}),
	}
}

// add makes an automation as ctx's person, with a run per result: a
// result starting with "!" is a failed run.
func (f resultsFixture) add(t *testing.T, ctx context.Context, name string, at time.Time, results ...string) automations.Automation {
	t.Helper()
	a, err := f.repo.Create(ctx, automations.CreateInput{
		Name: name, Prompt: "Summarize the news.", ModelID: "auto",
		Schedule: automations.Schedule{Kind: automations.KindManual, TimeZone: "UTC"},
	}, at.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range results {
		when := at.Add(time.Duration(i) * time.Minute)
		run, ok, err := f.repo.Claim(ctx, a.ID, when, when, time.Hour)
		if err != nil || !ok {
			t.Fatalf("claim: %v %v", ok, err)
		}
		if strings.HasPrefix(text, "!") {
			if err := f.repo.FailRun(ctx, run.ID, contracts.Failure{Text: text[1:], Code: "TEST"}, automations.Execution{}, when); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := f.repo.CompleteRun(ctx, run.ID, automations.Execution{Text: text}, when); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func resultRows(t *testing.T, result map[string]any) []map[string]any {
	t.Helper()
	list, ok := result["automations"].([]any)
	if !ok {
		t.Fatalf("automations = %#v", result["automations"])
	}
	out := make([]map[string]any, 0, len(list))
	for _, r := range list {
		out = append(out, r.(map[string]any))
	}
	return out
}

// automations.results reads the person's own latest results, newest first,
// with when each ran, and filters by name (toskar-apps#142).
func TestAutomationResultsReadsTheLatest(t *testing.T) {
	f := newResultsFixture(t)
	now := time.Date(2026, 11, 7, 18, 0, 0, 0, time.UTC) // 09:00 in Juneau
	f.add(t, f.sam, "Morning briefing", now.Add(-2*time.Hour), "Old briefing.", "Rain at noon; two meetings.")
	f.add(t, f.sam, "Price watch", now.Add(-26*time.Hour), "The lamp is $45.")
	f.add(t, f.sam, "Never ran", now)
	tool := &resultsTool{store: f.repo, now: func() time.Time { return now }}

	got, err := tool.Execute(f.sam, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	list := resultRows(t, got)
	if len(list) != 3 || list[0]["name"] != "Morning briefing" || list[1]["name"] != "Price watch" || list[2]["name"] != "Never ran" {
		t.Fatalf("rows = %v", list)
	}
	if list[0]["result"] != "Rain at noon; two meetings." || list[0]["when"] != "today at 07:01" {
		t.Fatalf("briefing = %v", list[0])
	}
	if list[1]["when"] != "yesterday at 07:00" {
		t.Fatalf("price watch = %v", list[1])
	}
	if list[2]["result"] != "" || list[2]["note"] == nil {
		t.Fatalf("never ran = %v", list[2])
	}

	got, _ = tool.Execute(f.sam, map[string]any{"name": "BRIEFING"})
	if list := resultRows(t, got); len(list) != 1 || list[0]["name"] != "Morning briefing" {
		t.Fatalf("by name = %v", list)
	}
	// A name nothing has gives them all, and says so.
	got, _ = tool.Execute(f.sam, map[string]any{"name": "weather"})
	if list := resultRows(t, got); len(list) != 3 || !strings.Contains(got["status"].(string), "weather") {
		t.Fatalf("unknown name = %v %v", list, got["status"])
	}
}

// The newest run failing is said, with the result from before it.
func TestAutomationResultsSaysALatestFailure(t *testing.T) {
	f := newResultsFixture(t)
	now := time.Date(2026, 11, 7, 18, 0, 0, 0, time.UTC)
	f.add(t, f.sam, "Morning briefing", now.Add(-time.Hour), "Sunny.", "!model ran out of memory")
	got, err := (&resultsTool{store: f.repo, now: func() time.Time { return now }}).Execute(f.sam, nil)
	if err != nil {
		t.Fatal(err)
	}
	list := resultRows(t, got)
	if len(list) != 1 || list[0]["result"] != "Sunny." || list[0]["latest_run_failed"] == nil {
		t.Fatalf("rows = %v", list)
	}
}

// With no automations, it says so and where to make one.
func TestAutomationResultsEmpty(t *testing.T) {
	f := newResultsFixture(t)
	got, err := (&resultsTool{store: f.repo}).Execute(f.sam, map[string]any{"name": "briefing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resultRows(t, got)) != 0 || !strings.Contains(got["status"].(string), "no automations") {
		t.Fatalf("result = %v", got)
	}
}

// Each person reads only their own automations' results: never another
// Member's, even by name, and a portal's guest or a Visitor reads none
// (#205, #206).
func TestAutomationResultsArePerPerson(t *testing.T) {
	f := newResultsFixture(t)
	now := time.Now().UTC().Add(-time.Hour)
	f.add(t, f.sam, "Sam's briefing", now, "Sam's private calendar.")
	f.add(t, f.ada, "Ada's briefing", now, "Ada's private notes.")
	tool := &resultsTool{store: f.repo}

	for _, tc := range []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"sam", f.sam, "Sam's briefing"},
		{"ada", f.ada, "Ada's briefing"},
	} {
		for _, filter := range []string{"", "briefing", "Sam", "Ada"} {
			got, err := tool.Execute(tc.ctx, map[string]any{"name": filter})
			if err != nil {
				t.Fatal(err)
			}
			list := resultRows(t, got)
			if len(list) != 1 || list[0]["name"] != tc.want {
				t.Fatalf("%s asking for %q got %v", tc.name, filter, list)
			}
		}
	}

	base := context.Background()
	for _, who := range []auth.Person{
		{ID: "guest-1", Role: auth.RoleVisitor, PortalID: "front-desk"},
		// A portal's guest never reads data, whatever its role says.
		{ID: "sam", Role: auth.RoleMember, PortalID: "front-desk"},
		{ID: "vic", Role: auth.RoleVisitor},
	} {
		ctx := auth.WithPrincipal(base, auth.Principal{Person: who, Via: auth.ViaSession})
		got, err := tool.Execute(ctx, map[string]any{"name": "briefing"})
		if err != nil {
			t.Fatal(err)
		}
		if list := resultRows(t, got); len(list) != 0 {
			t.Fatalf("%+v read %v", who, list)
		}
	}

	// A Visitor's chat isn't offered the tool at all: automations are off
	// for Visitors, and the tool goes with them.
	visitor := auth.DefaultCapabilities(auth.RoleVisitor)
	if blocked := blockedTools(visitor); !blocked["automations.results"] || blocked["news.read"] {
		t.Fatalf("visitor blocked = %v", blocked)
	}
	member := auth.DefaultCapabilities(auth.RoleMember)
	if blocked := blockedTools(member); blocked["automations.results"] || blocked["news.read"] {
		t.Fatalf("member blocked = %v", blocked)
	}
	if def, ok := tools.Lookup("automations.results"); !ok || def.Risk != tools.RiskRead || !def.Prefetch {
		t.Fatalf("catalog = %+v", def)
	}
}

// blockedTools is the tools caps turns off.
func blockedTools(caps auth.Capabilities) map[string]bool {
	out := map[string]bool{}
	for c, ids := range capabilityTools {
		if !caps.Allows(c) {
			for _, id := range ids {
				out[id] = true
			}
		}
	}
	return out
}
