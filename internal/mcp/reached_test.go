package mcp

import (
	"context"
	"testing"
)

// Sources that go online by themselves are recorded; one run directly on
// this computer isn't (#1415).
func TestReachedRecordsWhatGoesOnline(t *testing.T) {
	type rec struct {
		download bool
		to       string
	}
	var got []rec
	m := &Manager{Reached: func(_ context.Context, download bool, to, _ string) { got = append(got, rec{download, to}) }}
	ctx := context.Background()
	m.reached(ctx, Spec{Name: "Linear", URL: "https://mcp.linear.app/sse?token=x"}, "connected, and listed its tools")
	m.reached(ctx, Spec{Name: "Files", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem@1.2.0"}}, "connected, and listed its tools")
	m.reached(ctx, Spec{Name: "Files", Command: "npx"}, "listed its tools again, after they changed")
	m.reached(ctx, Spec{Name: "Time", Command: "/usr/local/bin/uvx", Args: []string{"mcp-server-time"}}, "connected, and listed its tools")
	m.reached(ctx, Spec{Name: "Mine", Command: "/opt/my-server"}, "connected, and listed its tools")
	want := []rec{{false, "Linear (mcp.linear.app)"}, {true, "the npm registry"}, {true, "PyPI"}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %v, want %v", i, got[i], want[i])
		}
	}
}
