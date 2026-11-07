package huginn

import (
	"slices"
	"testing"
)

// Asking for the AI news or a briefing, in each of the app's ten
// languages, as said in the car (toskar-apps#142).
func TestLookupCuesInEveryLanguage(t *testing.T) {
	news := map[string][]string{
		"en":      {"Read me today's AI news", "Any news about AI this morning?", "What's new in AI?"},
		"de":      {"Lies mir die KI-Nachrichten von heute vor", "Gibt es Neuigkeiten über KI?"},
		"es":      {"Léeme las noticias de IA de hoy", "¿Qué noticias hay sobre inteligencia artificial?"},
		"fr":      {"Lis-moi les actualités de l’IA du jour", "Quoi de neuf dans l'IA ?"},
		"it":      {"Leggimi le notizie sull'IA di oggi", "Quali sono le novità sull'IA?"},
		"pt-BR":   {"Leia as notícias de IA de hoje", "Quais são as novidades da IA?"},
		"ja":      {"今日のAIニュースを読んで", "ＡＩに関するニュースを教えて"},
		"ko":      {"오늘의 AI 뉴스 읽어줘", "인공지능 소식 알려줘"},
		"zh-Hans": {"给我读一下今天的AI新闻", "有什么人工智能资讯？"},
		"zh-Hant": {"幫我讀今天的AI新聞", "有什麼人工智慧新聞？"},
	}
	results := map[string][]string{
		"en":      {"What's in my briefing?", "Read me my morning briefing", "What did my automations say today?", "What did my automation Price watch find?"},
		"de":      {"Was steht in meinem Briefing?", "Lies mir das Ergebnis meiner Automatisierung vor"},
		"es":      {"¿Qué dice mi resumen de la mañana?", "Léeme los resultados de mis automatizaciones"},
		"fr":      {"Qu'y a-t-il dans mon briefing ?", "Lis-moi les résultats de mes automatisations"},
		"it":      {"Cosa c'è nel mio briefing?", "Leggimi i risultati delle mie automazioni"},
		"pt-BR":   {"O que tem no meu briefing?", "Leia os resultados das minhas automações"},
		"ja":      {"今日のブリーフィングを読んで", "自動化の結果を教えて"},
		"ko":      {"오늘 브리핑 읽어줘", "자동화 결과 알려줘"},
		"zh-Hans": {"我的简报里有什么？", "读一下自动化结果"},
		"zh-Hant": {"我的簡報裡有什麼？", "讀一下自動化結果"},
	}
	if len(news) != 10 || len(results) != 10 {
		t.Fatal("a case for each of the ten languages")
	}
	for lang, messages := range news {
		for _, m := range messages {
			if !AsksForNews(m) {
				t.Errorf("%s: %q isn't asking for the AI news", lang, m)
			}
			if got := ToolsFor(Chat, m, []string{"news.read", "automations.results"}); !slices.Contains(got, "news.read") {
				t.Errorf("%s: %q offered %v", lang, m, got)
			}
		}
	}
	for lang, messages := range results {
		for _, m := range messages {
			if !AsksForResults(m) {
				t.Errorf("%s: %q isn't asking for a result", lang, m)
			}
			if got := ToolsFor(Chat, m, []string{"news.read", "automations.results"}); !slices.Contains(got, "automations.results") {
				t.Errorf("%s: %q offered %v", lang, m, got)
			}
		}
	}
}

// Messages that talk about the news, briefs, or automation without asking
// for Toskar's own aren't offered the lookups.
func TestLookupCuesNearMisses(t *testing.T) {
	for _, m := range []string{
		"What's the latest news on the election?",
		"Write a news article about a cat that rescues a dog",
		"Read me the news",
		"Create an automation that checks prices every morning",
		"How does automation affect jobs?",
		"Help me write a legal brief",
		"Give me a briefing on the history of Rome",
		"I sent an email to Thai news outlets",
		"Was gibt es Neues in der Politik?",
		"Las noticias de hoy sobre fútbol",
		"Rédige un résumé de cette réunion",
		"今日のニュースは？",
		"오늘 날씨 어때?",
		"今天的新闻有什么？",
		"Paint a picture of a robot",
		"My company wants to add automation to the warehouse",
		"Summarize this briefing document for me",
	} {
		if AsksForNews(m) {
			t.Errorf("%q asks for the AI news", m)
		}
		if AsksForResults(m) {
			t.Errorf("%q asks for an automation's result", m)
		}
		if got := ToolsFor(Chat, m, []string{"news.read", "automations.results"}); len(got) != 0 {
			t.Errorf("%q offered %v", m, got)
		}
	}
}

// Every language's file loads and has both lists.
func TestLookupWordsLoad(t *testing.T) {
	w := lookupWords()
	if len(w.news) != 10 || len(w.results) != 10 {
		t.Fatalf("news %d, results %d languages; want 10 each", len(w.news), len(w.results))
	}
}
