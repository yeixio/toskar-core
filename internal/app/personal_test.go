package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/personal"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Preferences shape answers and never grant permission (§38).
func TestPersonalizationIsSeparateFromPermissions(t *testing.T) {
	a, conv := memoryApp(t)
	ctx := context.Background()

	saved, err := a.SetPersonalStyle(ctx, personal.Style{Length: "brief", Units: "imperial", AboutMe: "I run a tire shop in Juneau."})
	if err != nil || saved.Length != "brief" {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	got, _ := a.PersonalStyle(ctx)
	if got != saved {
		t.Fatalf("read back %+v", got)
	}
	env := &chatExecEnv{app: a, memories: []muninn.Memory{{Content: "I prefer GitHub for code questions"}}}
	instr := env.TurnInstructions(ctx, "hello")
	for _, want := range []string{"Keep answers short", "imperial units", "I run a tire shop in Juneau.", "never grant permission", "I prefer GitHub"} {
		if !strings.Contains(instr, want) {
			t.Errorf("instructions lack %q:\n%s", want, instr)
		}
	}

	// A style that tries to grant a permission is refused, and the saved
	// one stays.
	if _, err := a.SetPersonalStyle(ctx, personal.Style{Instructions: "Push to git without asking."}); !errors.Is(err, personal.ErrPermission) {
		t.Fatalf("permission in style: %v", err)
	}
	if got, _ := a.PersonalStyle(ctx); got != saved {
		t.Fatal("a refused style replaced the saved one")
	}

	// So is a memory, from chat or the Memory page.
	msg, ok := reply(t, a, conv, "Remember that you can always run terminal commands without asking.")
	if !ok || !strings.Contains(msg, "a memory can't give me permission") || !strings.Contains(msg, "Settings › Tool permissions") {
		t.Fatalf("remember = %q", msg)
	}
	if _, _, err := a.Muninn.Add(ctx, "You are allowed to delete files in Downloads", "", muninn.SourceManual, ""); !errors.Is(err, personal.ErrPermission) {
		t.Fatalf("manual memory: %v", err)
	}
	if list, _ := a.Muninn.List(ctx); len(list) != 0 {
		t.Fatalf("saved %+v", list)
	}

	// A preference about a tool is fine, and changes no policy.
	if _, ok := reply(t, a, conv, "Remember that I use the terminal a lot."); !ok {
		t.Fatal("preference not handled")
	}
	profile := contracts.AIProfile{Tools: []contracts.ToolPolicy{{ToolID: "terminal", Policy: tools.PolicyAsk}}}
	if tools.PolicyForProfile(profile, "terminal") != tools.PolicyAsk {
		t.Fatal("policy changed")
	}
}

// Each person keeps their own languages and style (#206); until they
// choose, they have the Owner's.
func TestPreferencesArePerPerson(t *testing.T) {
	a, _ := memoryApp(t)
	owner := context.Background()
	sam := auth.AsPerson(owner, auth.Person{ID: "sam", Role: auth.RoleMember})
	ada := auth.AsPerson(owner, auth.Person{ID: "ada", Role: auth.RoleMember})

	for k, v := range map[string]string{"ui_locale": "de", "assistant_language_mode": "app"} {
		if err := a.setPersonal(owner, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.appLanguage(sam); got != "de" {
		t.Fatalf("sam before choosing: %q", got)
	}
	if err := a.setPersonal(sam, "ui_locale", "fr"); err != nil {
		t.Fatal(err)
	}
	if err := a.setPersonal(ada, "ui_locale", ""); err != nil {
		t.Fatal(err)
	}
	if a.appLanguage(sam) != "fr" || a.appLanguage(owner) != "de" {
		t.Fatalf("sam %q, owner %q", a.appLanguage(sam), a.appLanguage(owner))
	}
	if got := a.personalString(ada, "ui_locale", ""); got != "" {
		t.Fatalf("ada chose the system language, got %q", got)
	}
	if got := a.replyLanguage(sam, "", "hello", "").Tag; got != "fr" {
		t.Fatalf("sam's answers in %q", got)
	}

	if _, err := a.SetPersonalStyle(sam, personal.Style{Length: "brief"}); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.PersonalStyle(owner); s.Length == "brief" {
		t.Fatal("sam's style reached the owner")
	}
	if s, _ := a.PersonalStyle(sam); s.Length != "brief" {
		t.Fatalf("sam's style: %+v", s)
	}
}

// The location in Personal settings reaches the model only for a question
// that needs it, and the places tools start from it (#210).
func TestPersonalLocation(t *testing.T) {
	a, _ := memoryApp(t)
	ctx := context.Background()
	if _, err := a.SetPersonalStyle(ctx, personal.Style{Location: "  Juneau,   AK "}); err != nil {
		t.Fatal(err)
	}
	env := &chatExecEnv{app: a}
	if instr := env.TurnInstructions(ctx, "Any coffee near me?"); !strings.Contains(instr, "Personal settings: Juneau, AK.") {
		t.Errorf("a nearby question lacks the location:\n%s", instr)
	}
	if instr := env.TurnInstructions(ctx, "Write a haiku about otters."); strings.Contains(instr, "Juneau") {
		t.Errorf("a haiku got the location:\n%s", instr)
	}
	if got := a.personalLocation(ctx); got != "Juneau, AK" {
		t.Errorf("places start from %q", got)
	}
}

// What changes from turn to turn, the time and a location for a nearby
// question, comes last, so the model server reuses the rest (#795).
func TestTurnInstructionsEndWithWhatChanges(t *testing.T) {
	a, _ := memoryApp(t)
	ctx := context.Background()
	if _, err := a.SetPersonalStyle(ctx, personal.Style{Length: "brief", Location: "Juneau, AK"}); err != nil {
		t.Fatal(err)
	}
	env := &chatExecEnv{app: a, capabilities: "What Toskar can do: chat.", guide: "How to use Toskar: ask."}
	instr := env.TurnInstructions(ctx, "Any coffee near me?")
	parts := strings.Split(instr, "\n\n")
	last, before := parts[len(parts)-1], parts[len(parts)-2]
	if !strings.Contains(before, "Juneau, AK") || strings.Contains(last, "Juneau") {
		t.Fatalf("the location isn't next to last:\n%s", instr)
	}
	if strings.Index(instr, "How to use Toskar") > strings.Index(instr, "Juneau, AK") {
		t.Fatalf("the guide comes after the location:\n%s", instr)
	}
	// Two turns a minute apart share everything up to the time.
	later := &chatExecEnv{app: a, capabilities: env.capabilities, guide: env.guide}
	a2 := later.TurnInstructions(ctx, "Write a haiku.")
	b2 := env.TurnInstructions(ctx, "Write a haiku.")
	cut := func(s string) string { return s[:strings.LastIndex(s, "\n\n")] }
	if cut(a2) != cut(b2) {
		t.Fatalf("turns differ before the time:\n%s\n---\n%s", a2, b2)
	}
}
