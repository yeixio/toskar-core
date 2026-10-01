package mimir

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Thresholds for passages found by meaning alone. Embedding models give
// unrelated text a similarity well above zero, so without a floor every
// question would bring in some passage. Measured with Nomic Embed Text v1.5
// on store policies and inventory rows: the right passage scored 0.64 to
// 0.77, and the best passage for an unrelated question at most 0.52.
const (
	// minSimilarity is the lowest cosine similarity a meaning-only match may
	// have.
	minSimilarity = 0.6
	// similarityWindow drops meaning-only matches below the best one by more
	// than this. Wrong passages for a policy question scored 0.05 to 0.1
	// under the right one.
	similarityWindow = 0.06
	// rrfK damps reciprocal rank fusion so a top rank in one list does not
	// outweigh good ranks in both.
	rrfK = 60
	// rerankTop is how many fused passages a reranker reorders.
	rerankTop = 16
)

// semanticResult is a meaning search: hits by similarity, best first, and the
// similarity of every passage that has a vector.
type semanticResult struct {
	hits []Hit
	sim  map[int64]float64
}

// semanticSearch ranks passages by cosine similarity to the question. ok is
// false when no embedding model can be used, and search stays keyword-only.
func (s *Store) semanticSearch(ctx context.Context, models Models, query string, ids []string, limit int) (semanticResult, bool) {
	if models == nil || query == "" {
		return semanticResult{}, false
	}
	emb, err := models.Embedder(ctx)
	if err != nil || emb == nil {
		return semanticResult{}, false
	}
	qv, err := emb.EmbedQuery(ctx, query)
	if err != nil {
		return semanticResult{}, false
	}
	q, ok := decodeVector(encodeVector(qv))
	if !ok {
		return semanticResult{}, false
	}
	res := semanticResult{sim: map[int64]float64{}}

	args := append([]any{emb.ModelID()}, anySlice(ids)...)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT chunk_rowid, vec FROM knowledge_vectors WHERE model = ? AND source_id IN (%s)`, marks(len(ids))), args...)
	if err != nil {
		return semanticResult{}, false
	}
	type scored struct {
		rowid int64
		sim   float64
	}
	var all []scored
	for rows.Next() {
		var rowid int64
		var blob []byte
		if err := rows.Scan(&rowid, &blob); err != nil {
			rows.Close()
			return semanticResult{}, false
		}
		v, ok := decodeVector(blob)
		if !ok {
			continue
		}
		sim := q.cosine(v)
		res.sim[rowid] = sim
		all = append(all, scored{rowid, sim})
	}
	rows.Close()
	if rows.Err() != nil {
		return semanticResult{}, false
	}
	sort.Slice(all, func(i, j int) bool { return all[i].sim > all[j].sim })
	if len(all) > limit {
		all = all[:limit]
	}
	if len(all) == 0 {
		// The model is installed but nothing is embedded yet.
		return res, true
	}

	rowids := make([]any, len(all))
	for i, a := range all {
		rowids[i] = a.rowid
	}
	rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT f.rowid, f.source_id, COALESCE(src.name, ''), f.title, f.body
		FROM knowledge_fts f
		LEFT JOIN knowledge_sources src ON src.id = f.source_id
		WHERE f.rowid IN (%s)`, marks(len(rowids))), rowids...)
	if err != nil {
		return semanticResult{}, false
	}
	defer rows.Close()
	byRow := map[int64]Hit{}
	for rows.Next() {
		h := Hit{Match: MatchSemantic}
		if err := rows.Scan(&h.rowid, &h.SourceID, &h.SourceName, &h.Title, &h.Body); err != nil {
			return semanticResult{}, false
		}
		byRow[h.rowid] = h
	}
	if rows.Err() != nil {
		return semanticResult{}, false
	}
	for _, a := range all {
		if h, ok := byRow[a.rowid]; ok {
			h.Score = a.sim
			res.hits = append(res.hits, h)
		}
	}
	return res, true
}

// alsoRelevant returns the meaning matches worth adding to the keyword ones:
// above the floor and near the best match. When keywords found
// something, a meaning-only match must also be at least as close to the
// question as the best keyword match, so a question naming one product does
// not bring in every similar row.
func (r semanticResult) alsoRelevant(keyword []Hit) []Hit {
	if len(r.hits) == 0 {
		return nil
	}
	cut := max(minSimilarity, r.hits[0].Score-similarityWindow)
	inKeyword := map[int64]bool{}
	for _, h := range keyword {
		inKeyword[h.rowid] = true
	}
	if len(keyword) > 0 {
		if sim, ok := r.sim[keyword[0].rowid]; ok {
			cut = max(cut, sim)
		}
	}
	var out []Hit
	for _, h := range r.hits {
		// A passage keywords found counts as agreement whatever its similarity.
		if inKeyword[h.rowid] || h.Score >= cut {
			out = append(out, h)
		}
	}
	return out
}

// fuse combines keyword and meaning rankings with reciprocal rank fusion: a
// passage scores 1/(60+rank) in each list it appears in, so passages both
// find rise to the top.
func fuse(keyword, semantic []Hit) []Hit {
	byRow := map[int64]*Hit{}
	var order []int64
	add := func(list []Hit, match string) {
		for i, h := range list {
			score := 1 / float64(rrfK+i+1)
			if cur, ok := byRow[h.rowid]; ok {
				cur.Score += score
				cur.Match = MatchBoth
				continue
			}
			h.Score, h.Match = score, match
			byRow[h.rowid] = &h
			order = append(order, h.rowid)
		}
	}
	add(keyword, MatchKeyword)
	add(semantic, MatchSemantic)
	out := make([]Hit, len(order))
	for i, id := range order {
		out[i] = *byRow[id]
	}
	// Stable, so a tie keeps the keyword match first.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// rerank reorders the top passages with an installed reranker model. Without
// one, or if it fails, the order is unchanged.
func (s *Store) rerank(ctx context.Context, models Models, query string, hits []Hit) []Hit {
	if models == nil || len(hits) < 2 || strings.TrimSpace(query) == "" {
		return hits
	}
	rr, err := models.Reranker(ctx)
	if err != nil || rr == nil {
		return hits
	}
	n := min(len(hits), rerankTop)
	texts := make([]string, n)
	for i := range n {
		texts[i] = embedText(hits[i].Title, hits[i].Body)
	}
	scores, err := rr.Rerank(ctx, query, texts)
	if err != nil || len(scores) != n {
		return hits
	}
	top := append([]Hit(nil), hits[:n]...)
	for i := range top {
		top[i].Score = scores[i]
	}
	sort.SliceStable(top, func(i, j int) bool { return top[i].Score > top[j].Score })
	return append(top, hits[n:]...)
}
