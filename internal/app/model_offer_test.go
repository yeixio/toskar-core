package app

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Why an answer could have been better, and when it couldn't (#707).
func TestOfferReason(t *testing.T) {
	small := contracts.Model{ID: "llama-3b", DisplayName: "Llama 3.2 3B", Parameters: "3B", Tags: []string{"general"}, MemoryNeeded: 3e9, Context: 8192}
	large := contracts.Model{ID: "qwen-14b", DisplayName: "Qwen 2.5 14B", Parameters: "14B", Tags: []string{"general"}, MemoryNeeded: 14e9, Context: 32768}
	coder := contracts.Model{ID: "coder-7b", Tags: []string{"coding"}, Capabilities: contracts.ModelCapabilities{Coding: true}}
	thinker := contracts.Model{ID: "r1-7b", Tags: []string{"reasoning"}}
	if !huginn.Small(small) || huginn.Small(large) {
		t.Fatal("test models: llama-3b should be small and qwen-14b not")
	}

	cases := []struct {
		name      string
		s         offerSignals
		answered  contracts.Model
		installed []contracts.Model
		want      string
		offerable contracts.Model // a model the reason should want
	}{
		{"coding with no coding model", offerSignals{kind: huginn.Coding}, small, []contracts.Model{small}, offerCoding, coder},
		{"coding with one installed", offerSignals{kind: huginn.Coding}, small, []contracts.Model{small, coder}, "", contracts.Model{}},
		{"files on a small model", offerSignals{kind: huginn.Research, dataKind: "file"}, small, []contracts.Model{small}, offerData, large},
		{"files with a larger one installed", offerSignals{kind: huginn.Research, dataKind: "file"}, small, []contracts.Model{small, large}, "", contracts.Model{}},
		{"files on a large model", offerSignals{kind: huginn.Research, dataKind: "knowledge"}, large, []contracts.Model{large}, "", contracts.Model{}},
		{"math at Thorough", offerSignals{kind: huginn.Chat, effort: huginn.EffortThorough, message: "What is 17 * 23?"}, small, []contracts.Model{small}, offerReasoning, thinker},
		{"math at Auto", offerSignals{kind: huginn.Chat, effort: huginn.EffortAuto, message: "What is 17 * 23?"}, small, []contracts.Model{small}, "", contracts.Model{}},
		{"math with a reasoning model installed", offerSignals{kind: huginn.Chat, effort: huginn.EffortThorough, message: "solve x + 2 = 5"}, small, []contracts.Model{small, thinker}, "", contracts.Model{}},
		{"a long chat", offerSignals{kind: huginn.Chat, longChat: true}, small, []contracts.Model{small}, offerContext, large},
		{"a quick question", offerSignals{kind: huginn.Chat, message: "hi"}, small, []contracts.Model{small}, "", contracts.Model{}},
	}
	for _, c := range cases {
		reason, wanted := offerReason(c.s, c.answered, c.installed)
		if reason != c.want {
			t.Errorf("%s: reason %q, want %q", c.name, reason, c.want)
			continue
		}
		if reason != "" && !wanted(c.offerable) {
			t.Errorf("%s: %s isn't wanted", c.name, c.offerable.ID)
		}
		if reason != "" && wanted(c.answered) && reason != offerLanguage {
			t.Errorf("%s: the model that answered is wanted", c.name)
		}
	}

	// A language the model writes weakly wants one rated good in it.
	es := contracts.Model{ID: "es-7b", Tags: []string{"general"}, Languages: []contracts.LanguageCapability{{Language: "es", Level: "excellent"}}}
	reason, wanted := offerReason(offerSignals{weakLang: true, lang: "es"}, small, []contracts.Model{small})
	if reason != offerLanguage || !wanted(es) || wanted(large) {
		t.Fatalf("language: %q wanted(es)=%v wanted(large)=%v", reason, wanted(es), wanted(large))
	}
}

func TestBetterPrefersTheLanguageThenSize(t *testing.T) {
	es := contracts.Model{ID: "es", MemoryNeeded: 4e9, Languages: []contracts.LanguageCapability{{Language: "es", Level: "excellent"}}}
	big := contracts.Model{ID: "big", MemoryNeeded: 14e9, Languages: []contracts.LanguageCapability{{Language: "es", Level: "fair"}}}
	if !better(es, big, "es") || better(es, big, "") {
		t.Fatal("better should prefer the language first, then size")
	}
}
