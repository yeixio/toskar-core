package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestParseTime(t *testing.T) {
	alaska := time.FixedZone("AKST", -9*60*60)
	for _, tc := range []struct {
		in   string
		want time.Time
	}{
		{"2026-11-01T12:30:45Z", time.Date(2026, 11, 1, 12, 30, 45, 0, time.UTC)},
		{"2026-11-01T12:30:45.123456789Z", time.Date(2026, 11, 1, 12, 30, 45, 123456789, time.UTC)},
		{"2026-11-01T03:30:45-09:00", time.Date(2026, 11, 1, 3, 30, 45, 0, alaska).UTC()},
		{"2026-11-01 12:30:45", time.Date(2026, 11, 1, 12, 30, 45, 0, time.UTC)},
		{"", time.Time{}},
		{"yesterday", time.Time{}},
		{"2026-11-01", time.Time{}},
	} {
		got := parseTime(tc.in)
		if !got.Equal(tc.want) {
			t.Errorf("parseTime(%q) = %v, want %v", tc.in, got, tc.want)
		}
		if !got.IsZero() && got.Location() != time.UTC {
			t.Errorf("parseTime(%q) is in %v, want UTC", tc.in, got.Location())
		}
	}
}

func TestNullStr(t *testing.T) {
	if got := nullStr(""); got != nil {
		t.Errorf("nullStr(\"\") = %#v, want nil", got)
	}
	if got := nullStr("x"); got != "x" {
		t.Errorf("nullStr(\"x\") = %#v, want \"x\"", got)
	}
}

func TestModelForRole(t *testing.T) {
	for _, tc := range []struct {
		name  string
		roles []contracts.ModelRole
		role  string
		want  string
	}{
		{"own model", []contracts.ModelRole{{Role: "assistant", ModelID: "a"}, {Role: "worker", ModelID: "w"}}, "worker", "w"},
		{"a worker slot uses the worker's model", []contracts.ModelRole{{Role: "assistant", ModelID: "a"}, {Role: "worker", ModelID: "w"}}, "worker:2", "w"},
		{"falls back to the first role with a model", []contracts.ModelRole{{Role: "planner"}, {Role: "assistant", ModelID: "a"}}, "worker", "a"},
		{"a role without a model falls back too", []contracts.ModelRole{{Role: "worker"}, {Role: "assistant", ModelID: "a"}}, "worker", "a"},
		{"no role has a model", []contracts.ModelRole{{Role: "assistant"}}, "assistant", ""},
		{"no roles", nil, "assistant", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &execEnv{profile: profiles.Profile{Roles: tc.roles}}
			if got := e.modelForRole(tc.role); got != tc.want {
				t.Fatalf("modelForRole(%q) = %q, want %q", tc.role, got, tc.want)
			}
		})
	}
}

func TestEnsureModelRunningNeedsAModel(t *testing.T) {
	_, err := (&Manager{}).ensureModelRunning(context.Background(), "")
	if code, _ := contracts.ErrorCode(err); code != "NO_MODEL_ASSIGNED" {
		t.Fatalf("ensureModelRunning(\"\") = %v, want NO_MODEL_ASSIGNED", err)
	}
}
