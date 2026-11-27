package app

import (
	"context"
	"strings"

	"github.com/yeixio/toskar-core/internal/voice"
)

// What voice mode says when an answer has to wait on a tool (#1487): a few
// words, as a person would say while they go and check, so the pause reads
// as Toskar looking it up rather than not hearing. Mike asked for it on
// 2026-11-27, with the thinking bubbles (#1334) playing around it.
type fillerSet struct {
	// web is for looking on the internet, places, maps, and the news;
	// data for the person's files, sheets, and connected services; other
	// for any other tool.
	web, data, other []string
}

var voiceFillers = map[string]fillerSet{
	"en": {
		web:   []string{"Hmm, let me look that up.", "Let me check on that.", "One sec, I'll look that up."},
		data:  []string{"Hmm, let me check.", "Let me have a look.", "One sec, I'll check."},
		other: []string{"Hmm, one moment.", "Let me work on that.", "Give me a second."},
	},
	"de": {
		web:   []string{"Hm, ich schau mal nach.", "Moment, ich suche das kurz."},
		data:  []string{"Hm, ich schau mal.", "Moment, ich sehe nach."},
		other: []string{"Hm, einen Moment.", "Moment bitte."},
	},
	"es": {
		web:   []string{"Mmm, déjame buscarlo.", "Un momento, lo busco."},
		data:  []string{"Mmm, déjame ver.", "Un momento, lo reviso."},
		other: []string{"Mmm, un momento.", "Dame un segundo."},
	},
	"fr": {
		web:   []string{"Hum, je regarde ça.", "Un instant, je cherche."},
		data:  []string{"Hum, je vérifie.", "Un instant, je regarde."},
		other: []string{"Hum, un instant.", "Une seconde."},
	},
	"it": {
		web:   []string{"Mmm, lo cerco subito.", "Un attimo, controllo."},
		data:  []string{"Mmm, fammi vedere.", "Un attimo, controllo."},
		other: []string{"Mmm, un attimo.", "Dammi un secondo."},
	},
	"pt": {
		web:   []string{"Hum, deixa eu procurar.", "Um momento, vou ver isso."},
		data:  []string{"Hum, deixa eu ver.", "Um momento, vou conferir."},
		other: []string{"Hum, um momento.", "Só um segundo."},
	},
	"ja": {
		web:   []string{"えっと、調べてみますね。", "ちょっと調べますね。"},
		data:  []string{"えっと、確認しますね。", "ちょっと見てみますね。"},
		other: []string{"えっと、少々お待ちください。", "ちょっと待ってくださいね。"},
	},
	"ko": {
		web:   []string{"음, 찾아볼게요.", "잠깐만요, 확인해 볼게요."},
		data:  []string{"음, 확인해 볼게요.", "잠깐만요, 볼게요."},
		other: []string{"음, 잠시만요.", "잠깐만 기다려 주세요."},
	},
	"zh": {
		web:   []string{"嗯，我查一下。", "稍等，我找一找。"},
		data:  []string{"嗯，我看一下。", "稍等，我确认一下。"},
		other: []string{"嗯，稍等一下。", "请稍等。"},
	},
}

// fillerKind is which of a fillerSet's lists suits a tool.
func fillerKind(set fillerSet, toolID string) []string {
	group, _, _ := strings.Cut(toolID, ".")
	switch group {
	case "internet", "web", "browser", "news", "places", "maps":
		return set.web
	case "filesystem", "spreadsheet", "svc", "automations", "github", "git":
		return set.data
	}
	return set.other
}

// filler is the next thing to say while toolID runs, in lang, or English
// when there are none in it. Each turn's is a different one.
func (c *voiceChat) filler(lang, toolID string) string {
	base, _, _ := strings.Cut(strings.ToLower(lang), "-")
	set, ok := voiceFillers[base]
	if !ok {
		set = voiceFillers["en"]
	}
	list := fillerKind(set, toolID)
	c.mu.Lock()
	n := c.fillers
	c.fillers++
	c.mu.Unlock()
	return list[n%len(list)]
}

// fillerLanguage is the language the answer will be in, so the filler and
// the answer are read in one voice: the chat's choice, else the language
// voice mode heard, else English.
func (c *voiceChat) fillerLanguage(ctx context.Context, text string) string {
	if lang := c.AnswerLanguage(ctx, text); lang != "" {
		return lang
	}
	if heard := voice.HeardLanguage(ctx); heard != "" {
		return heard
	}
	return "en"
}
