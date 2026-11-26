package speech

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
)

// echoSpeech answers a request's voice and speed as its audio.
const echoSpeech = `import base64, json, sys
for line in sys.stdin:
    req = json.loads(line)
    said = json.dumps({"voice": req.get("voice"), "speed": req.get("speed", 1)})
    print(json.dumps({"id": req["id"], "pcm": base64.b64encode(said.encode()).decode(), "rate": 22050}), flush=True)
`

// A persona reads its own language in its voice, and every language at its
// speed; a voice the request asks for reads as asked (#1319).
func TestPersonaReadsInItsVoiceAndSpeed(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	de, _ := VoiceFor("de")
	en, _ := VoiceFor("en")
	persona := Persona{ID: "p", Name: "Greta", Language: "de", Voice: de, Speed: 1.5}
	picked := true
	w := &Worker{Engine: &Engine{Python: fakeEnv{path: py}, Dir: t.TempDir()}, Script: []byte(echoSpeech), Plain: true,
		Persona: func(context.Context) (Persona, bool) { return persona, picked }}
	t.Cleanup(w.Close)
	said := func(text, voice, lang string) (string, float64) {
		t.Helper()
		pcm, _, err := w.Speak(context.Background(), text, voice, lang)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Voice string  `json:"voice"`
			Speed float64 `json:"speed"`
		}
		if err := json.Unmarshal(pcm, &got); err != nil {
			t.Fatalf("%s: %v", pcm, err)
		}
		return got.Voice, got.Speed
	}
	if v, s := said("Guten Tag.", "", "de"); v != de || s != 1.5 {
		t.Fatalf("its own language: %s at %v", v, s)
	}
	if v, s := said("Hello there.", "", "en"); v != en || s != 1.5 {
		t.Fatalf("another language: %s at %v", v, s)
	}
	if v, s := said("Guten Tag.", en, "de"); v != en || s != 1 {
		t.Fatalf("a voice asked for: %s at %v", v, s)
	}
	if v, s := said("Guten Tag.", "", "de"); v != de || s != 1.5 {
		t.Fatalf("again: %s at %v", v, s)
	}
	picked = false
	if _, s := said("Guten Tag.", "", "de"); s != 1 {
		t.Fatalf("no persona: at %v", s)
	}
	if _, s := said("Hello.", en, "en"); s != 1 {
		t.Fatalf("no persona, asked voice: at %v", s)
	}
	ctx := WithSpeed(context.Background(), 0.75)
	pcm, _, _ := w.Speak(ctx, "Hello.", en, "en")
	if string(pcm) != `{"voice": "`+en+`", "speed": 0.75}` {
		t.Fatalf("a speed asked for: %s", pcm)
	}
	if WithSpeed(context.Background(), 3) != context.Background() {
		t.Fatal("a speed out of range was kept")
	}
}
