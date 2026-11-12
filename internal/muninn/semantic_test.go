package muninn

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// conceptEmbedder stands in for a multilingual embedding model: words that
// mean the same thing in different languages land on the same dimension.
type conceptEmbedder struct {
	model string
	mu    sync.Mutex
	docs  int
}

var concepts = [][]string{
	{"project", "proyecto", "projet", "projekt"},
	{"go", "golang"},
	{"language", "lenguaje", "langage", "sprache"},
	{"coffee", "café", "kaffee"},
	{"dog", "perro", "hund"},
}

func (e *conceptEmbedder) ModelID() string { return e.model }

func (e *conceptEmbedder) vector(text string) []float32 {
	v := make([]float32, len(concepts)+1)
	v[len(concepts)] = 0.1 // so no vector is zero
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return r == ' ' || r == '.' || r == '?' || r == ',' }) {
		for i, words := range concepts {
			for _, c := range words {
				if w == c {
					v[i]++
				}
			}
		}
	}
	return v
}

func (e *conceptEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.docs += len(texts)
	e.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = e.vector(t)
	}
	return out, nil
}

func (e *conceptEmbedder) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	return e.vector(text), nil
}

func ids(ms []Memory) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Content
	}
	return out
}

func has(ms []Memory, content string) bool {
	for _, m := range ms {
		if m.Content == content {
			return true
		}
	}
	return false
}

// The spec's example (§18): a memory in Spanish answers a question in English.
func TestMemoryAcrossLanguages(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	m, _, err := s.Add(ctx, "Mi proyecto usa Go y lo despliego con Docker.", CategoryProjects, SourceExplicit, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Language != "es" {
		t.Fatalf("language = %q", m.Language)
	}
	if _, _, err := s.Add(ctx, "Mi perro se llama Toby.", CategoryOther, SourceExplicit, ""); err != nil {
		t.Fatal(err)
	}
	const question = "What language does my project use?"

	// By words alone, the English question can't find it.
	got, _ := s.Relevant(ctx, question, "")
	if has(got, m.Content) {
		t.Fatalf("found without meaning: %v", ids(got))
	}

	// By meaning, it can, and the unrelated memory stays out.
	emb := &conceptEmbedder{model: "multilingual"}
	s.SetEmbedder(func(context.Context) (Embedder, error) { return emb, nil })
	got, _ = s.Relevant(ctx, question, "")
	if !has(got, m.Content) || has(got, "Mi perro se llama Toby.") {
		t.Fatalf("by meaning: %v", ids(got))
	}

	// Vectors are kept: a second question embeds nothing new.
	before := emb.docs
	_, _ = s.Relevant(ctx, "Which project uses Go?", "")
	if emb.docs != before {
		t.Fatalf("re-embedded %d memories", emb.docs-before)
	}
	// An edit, or another embedding model, embeds again.
	edited := "Mi proyecto usa Go."
	if _, err := s.Update(ctx, m.ID, Patch{Content: &edited}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Relevant(ctx, question, "")
	if emb.docs != before+1 {
		t.Fatalf("after an edit, embedded %d", emb.docs-before)
	}
	other := &conceptEmbedder{model: "other"}
	s.SetEmbedder(func(context.Context) (Embedder, error) { return other, nil })
	_, _ = s.Relevant(ctx, question, "")
	if other.docs != 2 {
		t.Fatalf("a new model embedded %d memories", other.docs)
	}
	// A deleted memory's vector goes with it.
	if err := s.Delete(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM memory_vectors WHERE memory_id = ?`, m.ID).Scan(&n)
	if n != 0 {
		t.Fatal("the deleted memory's vector stayed")
	}
}

// No embedding model, or one that can't run now: memory is found by words.
func TestMemoryWithoutEmbeddings(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, _, err := s.Add(ctx, "My project uses Go.", CategoryProjects, SourceExplicit, ""); err != nil {
		t.Fatal(err)
	}
	s.SetEmbedder(func(context.Context) (Embedder, error) { return nil, nil })
	if got, err := s.Relevant(ctx, "Which project uses Go?", ""); err != nil || !has(got, "My project uses Go.") {
		t.Fatalf("by words: %v %v", ids(got), err)
	}
}
