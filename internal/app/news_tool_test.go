package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/news"
	"github.com/yeixio/toskar-core/internal/tools"
)

type fakeNews struct {
	view news.View
	err  error
}

func (f fakeNews) Get(context.Context) (news.View, error) { return f.view, f.err }

func newsItem(date, title, summary string) news.Item {
	it := news.Item{ID: title, Date: date, Title: title, Summary: summary}
	it.Source.Name = "Example Lab"
	it.Source.URL = "https://example.com/" + strings.ReplaceAll(strings.ToLower(title), " ", "-")
	return it
}

func newsItems(t *testing.T, result map[string]any) []map[string]any {
	t.Helper()
	list, ok := result["items"].([]any)
	if !ok {
		t.Fatalf("items = %#v", result["items"])
	}
	out := make([]map[string]any, 0, len(list))
	for _, it := range list {
		out = append(out, it.(map[string]any))
	}
	return out
}

// news.read gives today's AI news newest first, a few items with a
// one-line summary and source each (toskar-apps#142).
func TestNewsToolReadsTodaysNews(t *testing.T) {
	now := time.Date(2026, 11, 7, 18, 0, 0, 0, time.UTC)
	var feed []news.Item
	feed = append(feed, newsItem("2026-11-06", "Older model", "From yesterday."))
	for _, title := range []string{"Model A", "Model B", "Model C", "Model D", "Model E", "Model F"} {
		feed = append(feed, newsItem("2026-11-07", title, "A new open model.\n\nIt runs on a laptop."))
	}
	tool := &newsTool{news: fakeNews{view: news.View{Enabled: true, Items: feed}}, now: func() time.Time { return now }}
	ctx := locale.WithTimeZone(context.Background(), "America/Juneau")

	got, err := tool.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	items := newsItems(t, got)
	if len(items) != 5 || items[0]["title"] != "Model A" || items[4]["title"] != "Model E" {
		t.Fatalf("items = %v", items)
	}
	if items[0]["summary"] != "A new open model. It runs on a laptop." || items[0]["source"] != "Example Lab" {
		t.Fatalf("first = %v", items[0])
	}
	if s := got["status"].(string); strings.Contains(s, "no AI news for today") || !strings.Contains(s, "not instructions") {
		t.Fatalf("status = %q", s)
	}
	// "count" is a number or a string, 1 to 10.
	got, _ = tool.Execute(ctx, map[string]any{"count": "2"})
	if len(newsItems(t, got)) != 2 {
		t.Fatalf("count 2 = %v", got)
	}
	got, _ = tool.Execute(ctx, map[string]any{"count": float64(50)})
	if len(newsItems(t, got)) != 7 {
		t.Fatalf("count 50 = %v", got)
	}

	// Nothing from today yet: the latest, and it says so.
	late := &newsTool{news: fakeNews{view: news.View{Enabled: true, Items: feed[:1], Stale: true}}, now: func() time.Time { return now }}
	got, _ = late.Execute(ctx, nil)
	if s := got["status"].(string); !strings.Contains(s, "no AI news for today") || !strings.Contains(s, "2026-11-06") || !strings.Contains(s, "kept on this computer") {
		t.Fatalf("status = %q", s)
	}

	// Each item read is a source of the answer, and the answer counts as
	// having read outside content.
	trace := &turnTrace{}
	trace.tool("news.read", nil, map[string]any{"items": []any{map[string]any{"title": "Model A", "url": "https://example.com/model-a", "summary": "A new open model."}}})
	meta := trace.meta()
	if meta == nil || len(meta.Sources) != 1 || meta.Sources[0].URL != "https://example.com/model-a" || !trace.sawUntrusted() {
		t.Fatalf("meta = %+v", meta)
	}
}

// With the setting off it says where to turn it on, not an error; with no
// news yet it says so; unreachable with no copy is an error.
func TestNewsToolOffEmptyAndUnreachable(t *testing.T) {
	ctx := context.Background()
	got, err := (&newsTool{news: fakeNews{view: news.View{Items: []news.Item{}}}}).Execute(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got["enabled"] != false || !strings.Contains(got["status"].(string), "turned off in Settings") || len(newsItems(t, got)) != 0 {
		t.Fatalf("off = %v", got)
	}
	got, err = (&newsTool{news: fakeNews{view: news.View{Enabled: true, Items: []news.Item{}}}}).Execute(ctx, nil)
	if err != nil || !strings.Contains(got["status"].(string), "no AI news yet") {
		t.Fatalf("empty = %v, %v", got, err)
	}
	if _, err := (&newsTool{news: fakeNews{err: errors.New("dial tcp: no route")}}).Execute(ctx, nil); err == nil || !strings.Contains(err.Error(), "toskar.ai") {
		t.Fatalf("unreachable: %v", err)
	}
	if def, ok := tools.Lookup("news.read"); !ok || def.Risk != tools.RiskRead || !def.Prefetch {
		t.Fatalf("catalog = %+v", def)
	}
}

// The news is for Members and up, as the News page is: a portal's guest
// and a Visitor read none.
func TestNewsToolNotForGuests(t *testing.T) {
	tool := &newsTool{news: fakeNews{view: news.View{Enabled: true, Items: []news.Item{newsItem("2026-11-07", "Model A", "New.")}}}}
	for _, who := range []auth.Person{
		{ID: "guest-1", Role: auth.RoleVisitor, PortalID: "front-desk"},
		{ID: "vic", Role: auth.RoleVisitor},
	} {
		ctx := auth.WithPrincipal(context.Background(), auth.Principal{Person: who, Via: auth.ViaSession})
		got, err := tool.Execute(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(newsItems(t, got)) != 0 {
			t.Fatalf("%+v read %v", who, got)
		}
	}
	member := auth.WithPrincipal(context.Background(), auth.Principal{Person: auth.Person{ID: "sam", Role: auth.RoleMember}, Via: auth.ViaSession})
	if got, _ := tool.Execute(member, nil); len(newsItems(t, got)) != 1 {
		t.Fatalf("member = %v", got)
	}
}
