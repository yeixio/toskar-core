package api

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A Member's phone doesn't read where this computer keeps its files, its
// address, or its node ID; an Admin and this computer do (#1264).
func TestSettingsForTrimsMembersKeys(t *testing.T) {
	full := contracts.SettingsView{DataDir: "/data", ModelsDir: "/models", LogsDir: "/logs", APIHost: "0.0.0.0", NodeID: "node-1", RemoteAccessAddress: "home.example.net", UILocale: "de", MemoryEnabled: true}
	member := auth.Principal{Via: auth.ViaAPIKey, Person: auth.Person{Role: auth.RoleMember}}
	got := settingsFor(member, full)
	if got.DataDir != "" || got.ModelsDir != "" || got.LogsDir != "" || got.APIHost != "" || got.NodeID != "" || got.RemoteAccessAddress != "" {
		t.Fatalf("a Member's key read %+v", got)
	}
	if got.UILocale != "de" || !got.MemoryEnabled {
		t.Fatalf("a Member's key lost what the apps read: %+v", got)
	}
	for _, p := range []auth.Principal{
		{Via: auth.ViaAPIKey, Person: auth.Person{Role: auth.RoleAdmin}},
		{Via: auth.ViaThisComputer, Person: auth.Person{Role: auth.RoleOwner}},
		{Via: auth.ViaSession, Person: auth.Person{Role: auth.RoleMember}},
	} {
		if got := settingsFor(p, full); got.DataDir != "/data" || got.NodeID != "node-1" {
			t.Fatalf("%s %s lost settings: %+v", p.Via, p.Person.Role, got)
		}
	}
}

// A chat that doesn't exist is 404 for its summary and compacting, not 500 (#1264).
func TestCompactStatusNotFound(t *testing.T) {
	if got := compactStatus(contracts.NewError("NOT_FOUND", nil, nil)); got != 404 {
		t.Fatalf("status %d", got)
	}
}
