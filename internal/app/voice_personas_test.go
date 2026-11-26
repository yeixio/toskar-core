package app

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// An Admin makes personas from this computer's voices, each person picks
// their own, and deleting one leaves whoever picked it with the language's
// voice (#1319).
func TestVoicePersonas(t *testing.T) {
	a := voiceLanguageApp(t)
	owner := context.Background()
	sam := auth.WithPrincipal(owner, auth.Principal{Person: auth.Person{ID: "sam", Role: auth.RoleMember}})
	de, _ := speech.VoiceFor("de")

	for _, bad := range []speech.Persona{
		{Name: "", Language: "de", Voice: de},
		{Name: "Greta", Language: "de", Voice: "kitten:de:nobody"},
		{Name: "Greta", Language: "de", Voice: de, Speed: 3},
	} {
		if _, err := a.createVoicePersona(owner, bad); err == nil {
			t.Fatalf("made %+v", bad)
		} else if code, _ := contracts.ErrorCode(err); code != "PERSONA_NAME_INVALID" && code != "PERSONA_SPEED_INVALID" && code != "INVALID_VOICE" {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	greta, err := a.createVoicePersona(owner, speech.Persona{Name: " Greta ", Language: "DE", Voice: de})
	if err != nil || greta.ID == "" || greta.Name != "Greta" || greta.Language != "de" || greta.Speed != 1 {
		t.Fatalf("made %+v, %v", greta, err)
	}
	if err := a.pickVoicePersona(sam, "nobody"); err == nil {
		t.Fatal("picked a persona that isn't there")
	}
	if err := a.pickVoicePersona(sam, greta.ID); err != nil {
		t.Fatal(err)
	}
	if p, ok := a.pickedPersona(sam); !ok || p.ID != greta.ID {
		t.Fatalf("Sam's pick: %+v %v", p, ok)
	}
	if _, ok := a.pickedPersona(owner); ok {
		t.Fatal("Sam's pick is the Owner's too")
	}
	list, err := a.voicePersonas(sam)
	if err != nil || len(list.Personas) != 1 || list.Picked != greta.ID || !list.Personas[0].Reads {
		t.Fatalf("Sam's list: %+v, %v", list, err)
	}

	greta.Speed = 1.25
	if _, err := a.updateVoicePersona(owner, greta.ID, greta); err != nil {
		t.Fatal(err)
	}
	if p, _ := a.pickedPersona(sam); p.Speed != 1.25 {
		t.Fatalf("after the change: %+v", p)
	}
	if err := a.deleteVoicePersona(owner, greta.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.pickedPersona(sam); ok {
		t.Fatal("a deleted persona still reads")
	}
	if err := a.deleteVoicePersona(owner, greta.ID); err == nil {
		t.Fatal("deleted it twice")
	}
}
