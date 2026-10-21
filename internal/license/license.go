// Package license keeps what the Toskar Pro desktop app says about its
// purchase (yeixio/toskar-apps#336): a 14-day trial, unlocked, or neither.
// Core itself is free and has no license; a Core started by anything else
// stays at None and nothing is limited. Pro's store build tells Core the
// state when it starts it and after a purchase, and Core keeps the last one
// in its data folder.
//
// When a trial has ended without the Unlock, what's set up keeps working,
// but new setup (installing models, creating automations, adding people)
// waits for the Unlock.
package license

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Kind is where the purchase stands.
type Kind string

const (
	// None: Core, or Pro from before the trial; nothing is limited.
	None Kind = ""
	// Trial: the store's trial, until TrialEnds.
	Trial Kind = "trial"
	// Unlocked: bought, shared by family, or bought before the trial existed.
	Unlocked Kind = "unlocked"
	// Ended: the trial is over and the Unlock isn't bought.
	Ended Kind = "ended"
)

// State is what Pro said, with the trial's end. Store is where Pro was
// bought: "mac" (the Mac App Store) or "windows" (the Microsoft Store),
// which decides how the Unlock is offered.
type State struct {
	Kind      Kind      `json:"kind"`
	TrialEnds time.Time `json:"trial_ends,omitzero"`
	Store     string    `json:"store,omitempty"`
}

// Limited reports whether new setup waits for the Unlock.
func (s State) Limited() bool { return s.Kind == Ended }

// at is the state at a moment: a trial past its end has ended.
func (s State) at(now time.Time) State {
	if s.Kind == Trial && !s.TrialEnds.IsZero() && !now.Before(s.TrialEnds) {
		s.Kind = Ended
	}
	return s
}

// ErrInvalid is a state Pro mustn't send.
var ErrInvalid = errors.New("license: unknown kind, or a trial without its end")

// Keeper holds the state and its file.
type Keeper struct {
	mu    sync.Mutex
	path  string
	state State
	now   func() time.Time
}

const fileName = "license.json"

// Open reads the state kept in dataDir; a missing or unreadable file is None.
func Open(dataDir string) *Keeper {
	k := &Keeper{path: filepath.Join(dataDir, fileName), now: time.Now}
	if raw, err := os.ReadFile(k.path); err == nil {
		var s State
		if json.Unmarshal(raw, &s) == nil && valid(s) {
			k.state = s
		}
	}
	return k
}

func valid(s State) bool {
	switch s.Kind {
	case None, Unlocked, Ended:
		return true
	case Trial:
		return !s.TrialEnds.IsZero()
	}
	return false
}

// Get is the state now.
func (k *Keeper) Get() State {
	if k == nil {
		return State{}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.state.at(k.now())
}

// Set keeps what Pro says, and writes it to the data folder.
func (k *Keeper) Set(s State) (State, error) {
	if !valid(s) {
		return State{}, ErrInvalid
	}
	if s.Kind != Trial {
		s.TrialEnds = time.Time{}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	raw, err := json.Marshal(s)
	if err != nil {
		return State{}, err
	}
	tmp := k.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return State{}, err
	}
	if err := os.Rename(tmp, k.path); err != nil {
		return State{}, err
	}
	k.state = s
	return s.at(k.now()), nil
}
