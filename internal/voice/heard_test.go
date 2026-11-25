package voice

import (
	"context"
	"testing"
)

// hearingSTT is speech to text that says which language it heard.
type hearingSTT struct {
	heard Heard
	asked []string
}

func (h *hearingSTT) Transcribe(ctx context.Context, wav []byte, language string) (string, error) {
	got, err := h.Hear(ctx, wav, language)
	return got.Text, err
}

func (h *hearingSTT) Hear(_ context.Context, _ []byte, language string) (Heard, error) {
	h.asked = append(h.asked, language)
	return h.heard, nil
}

// ctxLLM is a chat that keeps the language each turn was heard in.
type ctxLLM struct {
	fakeLLM
	heard chan string
}

func (c *ctxLLM) Respond(ctx context.Context, text string, delta func(string)) error {
	c.heard <- HeardLanguage(ctx)
	return c.fakeLLM.Respond(ctx, text, delta)
}

// A word or two said aloud is too short to tell its language from the
// text, so the answer follows the language speech to text heard, when it's
// sure of it (#1322), and the event says which one.
func TestSessionAnswersInTheLanguageHeard(t *testing.T) {
	for _, c := range []struct {
		name string
		sure float64
		want string
	}{
		{"sure", 0.93, "es"},
		{"unsure", 0.41, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			llm := &ctxLLM{fakeLLM: fakeLLM{deltas: []string{"¡Hola!"}}, heard: make(chan string, 1)}
			rec := newRecorder()
			s := NewSession(Parts{Transcriber: &hearingSTT{heard: Heard{Text: "¿Hola?", Language: "es", Confidence: c.sure}}, Responder: llm, Speaker: &fakeTTS{}}, rec.send)
			defer s.Close()
			speak(s, append(append(quiet(0.3), tone(0.6)...), quiet(1.1)...))
			rec.waitFor(t, "response.done")
			if got := <-llm.heard; got != c.want {
				t.Errorf("answered with heard language %q, want %q", got, c.want)
			}
			lang, _ := rec.last("conversation.item.input_audio_transcription.completed")["language"].(string)
			if lang != c.want {
				t.Errorf("transcription event language %q, want %q", lang, c.want)
			}
		})
	}
}

// A language the person set isn't second-guessed.
func TestHearKeepsAChosenLanguage(t *testing.T) {
	h := &hearingSTT{heard: Heard{Text: "Hallo", Language: "nl", Confidence: 0.99}}
	text, heard, err := Hear(context.Background(), h, nil, "de")
	if err != nil || text != "Hallo" || heard != "" {
		t.Fatalf("Hear = %q, %q, %v", text, heard, err)
	}
	if h.asked[0] != "de" {
		t.Fatalf("asked for %q", h.asked[0])
	}
	// A transcriber that can't say which language falls back to plain text.
	text, heard, err = Hear(context.Background(), &fakeSTT{text: "hi"}, nil, "")
	if err != nil || text != "hi" || heard != "" {
		t.Fatalf("plain Hear = %q, %q, %v", text, heard, err)
	}
}
