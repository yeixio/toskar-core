package mimir

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"strings"
	"sync"
	"testing"
)

// fakeEmbedder places words that mean the same thing on one axis, the way a
// real embedding model places synonyms close together. Other words land on
// hashed axes with a small weight.
type fakeEmbedder struct {
	model string
	mu    sync.Mutex
	// embedded counts passages sent for embedding.
	embedded int
	fail     error
}

var concepts = map[string]int{
	"warranty": 0, "guarantee": 0, "covered": 0,
	"tire": 1, "tires": 1, "tyre": 1, "tyres": 1,
	"snow": 2, "winter": 2, "blizzak": 2, "ice": 2,
	"refund": 3, "refunds": 3, "return": 3, "returns": 3, "money": 3,
}

const fakeDims = 64

func (f *fakeEmbedder) vector(text string) []float32 {
	v := make([]float32, fakeDims)
	for _, w := range termRe.FindAllString(strings.ToLower(text), -1) {
		if stopwords[w] {
			continue
		}
		if c, ok := concepts[w]; ok {
			v[c] += 3
			continue
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		v[8+int(h.Sum32()%(fakeDims-8))] += 0.4
	}
	return v
}

func (f *fakeEmbedder) ModelID() string { return f.model }

func (f *fakeEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	f.mu.Lock()
	f.embedded += len(texts)
	f.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = f.vector(t)
	}
	return out, nil
}

func (f *fakeEmbedder) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	return f.vector(text), nil
}

func (f *fakeEmbedder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.embedded
}

// fakeReranker prefers passages that mention a word.
type fakeReranker struct{ prefer string }

func (r fakeReranker) Rerank(_ context.Context, _ string, passages []string) ([]float64, error) {
	out := make([]float64, len(passages))
	for i, p := range passages {
		if strings.Contains(strings.ToLower(p), r.prefer) {
			out[i] = 5
		} else {
			out[i] = -float64(i)
		}
	}
	return out, nil
}

type fakeModels struct {
	emb    *fakeEmbedder
	rr     Reranker
	embErr error
}

func (m *fakeModels) Embedder(context.Context) (Embedder, error) {
	if m.embErr != nil {
		return nil, m.embErr
	}
	if m.emb == nil {
		return nil, nil
	}
	return m.emb, nil
}

func (m *fakeModels) Reranker(context.Context) (Reranker, error) { return m.rr, nil }

const policies = `# Store policies

## Guarantee

Every tyre we sell is covered for five years against defects.

## Returns

Unused items can be brought back within 30 days for a refund.

## Opening hours

We open at 9 and close at 5 on weekdays.
`

func semanticStore(t *testing.T) (*Store, *fakeModels) {
	t.Helper()
	s := newTestStore(t)
	m := &fakeModels{emb: &fakeEmbedder{model: "fake-embed"}}
	s.SetModels(m)
	return s, m
}

func TestSemanticSearchFindsPassageWithoutSharedWords(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "policies.md", Text: policies})
	if err != nil {
		t.Fatal(err)
	}
	q := SearchInput{Query: "What warranty do you offer?", SourceIDs: []string{src.ID}}

	// Keyword search alone shares no word with the guarantee passage.
	hits, err := s.Search(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("keyword search should find nothing, got %+v", hits)
	}

	m := &fakeModels{emb: &fakeEmbedder{model: "fake-embed"}}
	s.SetModels(m)
	if err := s.EmbedPending(ctx, nil); err != nil {
		t.Fatal(err)
	}
	src, _ = s.Get(ctx, src.ID)
	if src.EmbeddedCount != src.ChunkCount || src.EmbeddingModel != "fake-embed" {
		t.Fatalf("embedded %d of %d with %q", src.EmbeddedCount, src.ChunkCount, src.EmbeddingModel)
	}
	hits, err = s.Search(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Body, "five years") || hits[0].Match != MatchSemantic {
		t.Fatalf("want only the guarantee passage, found by meaning; got %+v", hits)
	}
}

func TestHybridRanksPassagesBothSearchesFindFirst(t *testing.T) {
	ctx := context.Background()
	s, _ := semanticStore(t)
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "policies.md", Text: policies})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EmbedPending(ctx, nil); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(ctx, SearchInput{Query: "refund for returns", SourceIDs: []string{src.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Body, "30 days") || hits[0].Match != MatchBoth {
		t.Fatalf("want the returns passage first, found both ways; got %+v", hits)
	}
}

func TestKeywordMatchKeepsSimilarRowsOut(t *testing.T) {
	ctx := context.Background()
	s, _ := semanticStore(t)
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "inventory.csv", Text: inventory + moreTires})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EmbedPending(ctx, nil); err != nil {
		t.Fatal(err)
	}
	// Every row is a tire, so they embed alike. Naming one SKU must not bring
	// in the other.
	hits, err := s.Search(ctx, SearchInput{Query: "price of MP-22545 tires", SourceIDs: []string{src.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Body, "MP-22545") {
		t.Fatalf("want only the named row, got %+v", hits)
	}
	// A question by meaning still finds the winter tire.
	hits, err = s.Search(ctx, SearchInput{Query: "something for snow and ice", SourceIDs: []string{src.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Body, "Blizzak") {
		t.Fatalf("want the Blizzak row, got %+v", hits)
	}
}

const moreTires = `CT-21555,Continental,ExtremeContact,215/55R17,165.00,6
GY-19565,Goodyear,Assurance,195/65R15,99.00,20
PI-23545,Pirelli,P Zero,235/45R18,210.00,3
`

func TestReindexKeepsVectorsOfUnchangedPassages(t *testing.T) {
	ctx := context.Background()
	s, m := semanticStore(t)
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "inventory.csv", Text: inventory})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EmbedPending(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if m.emb.count() != 2 {
		t.Fatalf("embedded %d passages, want 2", m.emb.count())
	}
	updated := strings.Replace(inventory, "189.99", "174.00", 1)
	if _, err := s.Update(ctx, src.ID, UpdateInput{Text: &updated}); err != nil {
		t.Fatal(err)
	}
	src, _ = s.Get(ctx, src.ID)
	if src.EmbeddedCount != 1 {
		t.Fatalf("the unchanged row should keep its vector; %d embedded", src.EmbeddedCount)
	}
	if err := s.EmbedPending(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if m.emb.count() != 3 {
		t.Fatalf("only the changed row should be embedded again; embedded %d in total", m.emb.count())
	}
}

func TestNewEmbeddingModelReembedsEverything(t *testing.T) {
	ctx := context.Background()
	s, m := semanticStore(t)
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "policies.md", Text: policies})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EmbedPending(ctx, nil); err != nil {
		t.Fatal(err)
	}
	m.emb = &fakeEmbedder{model: "other-embed"}
	if err := s.EmbedPending(ctx, nil); err != nil {
		t.Fatal(err)
	}
	src, _ = s.Get(ctx, src.ID)
	if m.emb.count() != src.ChunkCount || src.EmbeddingModel != "other-embed" || src.EmbeddedCount != src.ChunkCount {
		t.Fatalf("embedded %d; source reports %d with %q", m.emb.count(), src.EmbeddedCount, src.EmbeddingModel)
	}
}

func TestSearchFallsBackToKeywordsWhenTheModelCannotRun(t *testing.T) {
	ctx := context.Background()
	s, m := semanticStore(t)
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "inventory.csv", Text: inventory})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EmbedPending(ctx, nil); err != nil {
		t.Fatal(err)
	}

	m.embErr = ErrNotNow
	hits, err := s.Search(ctx, SearchInput{Query: "225/45R17", SourceIDs: []string{src.ID}})
	if err != nil || len(hits) != 1 || hits[0].Match != MatchKeyword {
		t.Fatalf("want a keyword hit, got %+v, %v", hits, err)
	}
	updated := inventory + "GY-1,Goodyear,UltraGrip,195/65R15,99.00,4\n"
	if _, err := s.Update(ctx, src.ID, UpdateInput{Text: &updated}); err != nil {
		t.Fatal(err)
	}
	if err := s.EmbedPending(ctx, nil); !errors.Is(err, ErrNotNow) {
		t.Fatalf("background embedding should wait, got %v", err)
	}

	m.embErr = nil
	m.emb.fail = errors.New("llama-server error 500")
	hits, err = s.Search(ctx, SearchInput{Query: "225/45R17", SourceIDs: []string{src.ID}})
	if err != nil || len(hits) != 1 {
		t.Fatalf("a failing embedding model must not break search: %+v, %v", hits, err)
	}
}

func TestRerankerOrdersTopPassages(t *testing.T) {
	ctx := context.Background()
	s, m := semanticStore(t)
	m.rr = fakeReranker{prefer: "bridgestone"}
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "inventory.csv", Text: inventory})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EmbedPending(ctx, nil); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(ctx, SearchInput{Query: "tires in stock", SourceIDs: []string{src.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 || !strings.Contains(hits[0].Body, "Bridgestone") || hits[0].Score != 5 {
		t.Fatalf("want the reranker's choice first, got %+v", hits)
	}
}

func TestBackgroundEmbeddingIsAdmittedPerBatch(t *testing.T) {
	ctx := context.Background()
	s, _ := semanticStore(t)
	var rows strings.Builder
	rows.WriteString("sku,name\n")
	for i := range embedBatch + 5 {
		rows.WriteString("SKU-" + strings.Repeat("x", i+1) + ",Item\n")
	}
	if _, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "items.csv", Text: rows.String()}); err != nil {
		t.Fatal(err)
	}
	admitted, released := 0, 0
	admit := func(context.Context) (func(), error) {
		admitted++
		return func() { released++ }, nil
	}
	if err := s.EmbedPending(ctx, admit); err != nil {
		t.Fatal(err)
	}
	if admitted != 2 || released != 2 {
		t.Fatalf("admitted %d and released %d times, want 2 batches", admitted, released)
	}

	busy := errors.New("training")
	s2, _ := semanticStore(t)
	if _, err := s2.Create(ctx, CreateInput{Kind: KindText, Filename: "policies.md", Text: policies}); err != nil {
		t.Fatal(err)
	}
	if err := s2.EmbedPending(ctx, func(context.Context) (func(), error) { return nil, busy }); !errors.Is(err, busy) {
		t.Fatalf("got %v", err)
	}
}

func TestDeleteRemovesVectors(t *testing.T) {
	ctx := context.Background()
	s, _ := semanticStore(t)
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "policies.md", Text: policies})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EmbedPending(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM knowledge_vectors`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d vectors left, %v", n, err)
	}
}

func TestQuantizedCosineIsClose(t *testing.T) {
	a := make([]float32, 768)
	b := make([]float32, 768)
	for i := range a {
		a[i] = float32(math.Sin(float64(i)))
		b[i] = float32(math.Sin(float64(i)) + 0.5*math.Cos(float64(i)*3))
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	want := dot / math.Sqrt(na*nb)
	qa, _ := decodeVector(encodeVector(a))
	qb, _ := decodeVector(encodeVector(b))
	if got := qa.cosine(qb); math.Abs(got-want) > 0.01 {
		t.Fatalf("cosine %f, want %f", got, want)
	}
	if encodeVector(make([]float32, 4)) != nil {
		t.Fatal("a zero vector has no direction")
	}
}

func TestFuseRewardsAgreement(t *testing.T) {
	kw := []Hit{{rowid: 1}, {rowid: 2}, {rowid: 3}}
	sem := []Hit{{rowid: 3}, {rowid: 4}}
	got := fuse(kw, sem)
	if got[0].rowid != 3 || got[0].Match != MatchBoth {
		t.Fatalf("the passage both lists found should lead: %+v", got)
	}
	if got[1].rowid != 1 || got[2].rowid != 2 || got[3].rowid != 4 {
		t.Fatalf("ties keep the keyword match first: %+v", got)
	}
}
