package replylang

import (
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := map[string]string{
		"What is the capital of France and why is it there?":                                      "en",
		"Kannst du mir bitte erklären, wie das funktioniert?":                                     "de",
		"¿Puedes explicarme cómo funciona esto, por favor?":                                       "es",
		"Pouvez-vous m'expliquer comment cela fonctionne ?":                                       "fr",
		"Puoi spiegarmi come funziona questo, per favore?":                                        "it",
		"Você pode me explicar como isso funciona, por favor?":                                    "pt",
		"これがどのように動作するか説明してください。":                                                                  "ja",
		"이것이 어떻게 작동하는지 설명해 주세요.":                                                                  "ko",
		"请解释一下这个是怎么工作的。":                                                                          "zh-Hans",
		"請解釋一下這個是怎麼運作的。":                                                                          "zh-Hant",
		"Объясните, пожалуйста, как это работает.":                                                "ru",
		"Kun je uitleggen hoe dit werkt, alsjeblieft?":                                            "nl",
		"Can you explain what this code does?\n```go\nfunc main() { fmt.Println(\"hola\") }\n```": "en",
	}
	for text, want := range cases {
		if got, ok := Detect(text); !ok || got != want {
			t.Errorf("Detect(%q) = %q, %v; want %q", text, got, ok, want)
		}
	}
	// Too short or not language at all.
	for _, text := range []string{"ok", "thanks!", "👍", "```\nls -la\n```", "https://example.com/a/b/c", "42"} {
		if got, ok := Detect(text); ok {
			t.Errorf("Detect(%q) = %q, want no guess", text, got)
		}
	}
}

func TestRequested(t *testing.T) {
	cases := map[string]string{
		"Answer this one in English.":                           "en",
		"Can you reply in German?":                              "de",
		"Explain photosynthesis. In Spanish, please.":           "es",
		"Antworte bitte auf Englisch.":                          "en",
		"Erkläre mir das, bitte auf Deutsch":                    "de",
		"Responde en inglés, por favor.":                        "en",
		"Réponds en anglais s'il te plaît.":                     "en",
		"Rispondi in inglese.":                                  "en",
		"Responda em inglês, por favor.":                        "en",
		"英語で答えてください。":                                           "en",
		"영어로 답해 주세요.":                                           "en",
		"请用英文回答。":                                               "en",
		"Answer in Traditional Chinese.":                        "zh-Hant",
		"Reply in German... actually, reply in French instead.": "fr",
	}
	for text, want := range cases {
		if got, ok := Requested(text); !ok || got != want {
			t.Errorf("Requested(%q) = %q, %v; want %q", text, got, ok, want)
		}
	}
	// Mentions of a language that are not asking for the answer in it.
	for _, text := range []string{
		"How do you say 'thank you' in Japanese?",
		"Wie sagt man das auf Englisch?",
		"¿Cómo se dice esto en inglés?",
		"I'm learning German.",
		"Translate 'hello' and tell me what it means.",
	} {
		if got, ok := Requested(text); ok {
			t.Errorf("Requested(%q) = %q, want none", text, got)
		}
	}
}

func TestResolve(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want Decision
	}{
		{"an explicit request always wins", Input{Mode: ModeLanguage, Setting: "de", App: "de", Message: "Answer this one in English."}, Decision{Tag: "en", Source: FromRequest}},
		{"auto follows the message", Input{App: "de", Message: "What is the tallest mountain in Europe?"}, Decision{Tag: "en", Source: FromMessage}},
		{"auto uses the conversation for a short message", Input{App: "en", Message: "ok, und dann?", History: []string{"Wie hoch ist die Zugspitze genau?"}}, Decision{Tag: "de", Source: FromConversation}},
		{"auto falls back to the App language", Input{Mode: ModeAuto, App: "fr", Message: "ok"}, Decision{Tag: "fr", Source: FromApp}},
		{"a chosen language wins over the message", Input{Mode: ModeLanguage, Setting: "es", App: "de", Message: "What is the tallest mountain in Europe?"}, Decision{Tag: "es", Source: FromSetting}},
		{"same as app", Input{Mode: ModeApp, App: "ja", Message: "What is the tallest mountain in Europe?"}, Decision{Tag: "ja", Source: FromApp}},
		{"same as app with the system language", Input{Mode: ModeApp, System: "it_IT", Message: "hi"}, Decision{Tag: "it-IT", Source: FromSystem}},
		{"pseudo-locales answer in English", Input{Mode: ModeApp, App: "en-XA", Message: "hi"}, Decision{Tag: "en", Source: FromDefault}},
		{"nothing to go on", Input{Message: "👍"}, Decision{Tag: "en", Source: FromDefault}},
		{"auto follows the language a short message was spoken in", Input{App: "en", Message: "¿Hola?", Spoken: "es"}, Decision{Tag: "es", Source: FromSpeech}},
		{"the text wins over the language heard", Input{App: "en", Message: "What is the tallest mountain in Europe?", Spoken: "pt"}, Decision{Tag: "en", Source: FromMessage}},
		{"a chosen language wins over the language heard", Input{Mode: ModeLanguage, Setting: "de", Message: "ok", Spoken: "es"}, Decision{Tag: "de", Source: FromSetting}},
	}
	for _, c := range cases {
		if got := Resolve(c.in); got != c.want {
			t.Errorf("%s: Resolve = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestInstruction(t *testing.T) {
	if got := (Decision{Tag: "zh-Hant"}).Instruction(); !strings.Contains(got, "Traditional Chinese") {
		t.Errorf("instruction = %q", got)
	}
	if got := Name("de"); got != "German" {
		t.Errorf("Name(de) = %q", got)
	}
}

// An answer read aloud in a language this computer has no voice for is
// written in the next language it can read, and says why (#1405).
func TestResolveSpeakable(t *testing.T) {
	speaks := func(tags ...string) func(string) bool {
		return func(tag string) bool {
			for _, s := range tags {
				if s == tag {
					return true
				}
			}
			return false
		}
	}
	// Heard Swahili, with a German App language: German, and why.
	d := Resolve(Input{App: "de", Message: "Habari, unaweza kunisaidia leo?", Spoken: "sw", Speakable: speaks("de", "en"),
		Offer: func(tag string) string { return "Kitten TTS in Settings › Voice" }})
	if d.Tag != "de" || d.Source != FromApp || d.Unspoken == "" || d.Offer == "" {
		t.Fatalf("decision = %+v", d)
	}
	if line := d.Instruction(); !strings.Contains(line, "can't speak") || !strings.Contains(line, "Kitten TTS") {
		t.Fatalf("instruction = %q", line)
	}
	// A language it can read is kept, and says nothing more.
	d = Resolve(Input{App: "de", Message: "¿Hola?", Spoken: "es", Speakable: speaks("de", "es")})
	if d.Tag != "es" || d.Unspoken != "" || strings.Contains(d.Instruction(), "aloud") {
		t.Fatalf("speakable = %+v", d)
	}
	// Nothing else it can read: English.
	d = Resolve(Input{App: "ja", Message: "ok", Speakable: speaks("en")})
	if d.Tag != "en" || d.Unspoken != "ja" {
		t.Fatalf("fallback = %+v", d)
	}
	// Written answers take any language.
	if d := Resolve(Input{App: "ja", Message: "ok"}); d.Tag != "ja" || d.Unspoken != "" {
		t.Fatalf("written = %+v", d)
	}
}
