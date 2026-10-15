package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/store"
)

// What each role and person may use (#589): shipped defaults, a role's
// changes, one person's Allow and Block, the Owner fixed, and a portal's
// guest as a Visitor.
func TestCapabilities(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	people := NewPeople(db.SQL)
	ctx := context.Background()

	if d := DefaultCapabilities(RoleMember); d[CapChangingTools] || !d[CapMakeVideo] {
		t.Fatalf("member defaults %v", d)
	}
	if d := DefaultCapabilities(RoleVisitor); d[CapFiles] || d[CapMakePictures] || !d[CapWeb] || !d[CapPicturesIn] {
		t.Fatalf("visitor defaults %v", d)
	}

	// Members lose video; Kim, a Member, gets it back, and loses the web.
	if err := people.SetRoleCapabilities(ctx, RoleMember, map[Capability]bool{CapMakeVideo: false, CapWeb: true}); err != nil {
		t.Fatal(err)
	}
	kim, _ := people.Create(ctx, "Kim", RoleMember)
	lee, _ := people.Create(ctx, "Lee", RoleMember)
	yes, no := true, false
	if err := people.SetPersonCapabilities(ctx, kim.ID, map[Capability]*bool{CapMakeVideo: &yes, CapWeb: &no}); err != nil {
		t.Fatal(err)
	}
	kimCaps, _ := people.CapabilitiesOf(ctx, kim)
	leeCaps, _ := people.CapabilitiesOf(ctx, lee)
	if !kimCaps.Allows(CapMakeVideo) || kimCaps.Allows(CapWeb) || leeCaps.Allows(CapMakeVideo) || !leeCaps.Allows(CapWeb) {
		t.Fatalf("kim %v, lee %v", kimCaps, leeCaps)
	}
	// Back to the role's for the web.
	if err := people.SetPersonCapabilities(ctx, kim.ID, map[Capability]*bool{CapMakeVideo: &yes, CapWeb: nil}); err != nil {
		t.Fatal(err)
	}
	if set, _ := people.PersonCapabilities(ctx, kim.ID); len(set) != 1 || !set[CapMakeVideo] {
		t.Fatalf("kim's own %v", set)
	}

	// The Owner always has everything, and can't be changed.
	owner, _ := people.Get(ctx, OwnerID)
	if caps, _ := people.CapabilitiesOf(ctx, owner); !caps.Allows(CapChangingTools) {
		t.Fatalf("owner %v", caps)
	}
	if err := people.SetRoleCapabilities(ctx, RoleOwner, map[Capability]bool{CapWeb: false}); err != ErrCapabilityRole {
		t.Fatalf("owner's role: %v", err)
	}
	if err := people.SetPersonCapabilities(ctx, OwnerID, map[Capability]*bool{CapWeb: &no}); err != ErrCapabilityRole {
		t.Fatalf("owner's own: %v", err)
	}
	if err := people.SetRoleCapabilities(ctx, RoleMember, map[Capability]bool{"teleport": true}); err != ErrCapabilityUnknown {
		t.Fatalf("unknown: %v", err)
	}

	// A portal's guest is a Visitor, whatever their own.
	guest := Person{ID: "g1", Role: RoleVisitor, PortalID: "p1"}
	if caps, _ := people.CapabilitiesOf(ctx, guest); caps.Allows(CapFiles) || !caps.Allows(CapWeb) {
		t.Fatalf("guest %v", caps)
	}
}
