package mimir

import (
	"context"
	"fmt"
	"time"
)

// Admit waits until background embedding may use the computer and returns a
// function that gives it back. It is called for each small batch, so a chat
// that starts while passages are being embedded goes first (spec §60).
type Admit func(ctx context.Context) (release func(), err error)

const (
	// embedBatch is how many passages go to the embedding model at once.
	embedBatch = 32
	// pendingPage is how many passages are read per scan for missing vectors.
	pendingPage = 512
	// indexRetry is how long background embedding waits after it could not
	// run, for example because training holds the computer.
	indexRetry = 2 * time.Minute
)

// StartIndexing embeds passages in the background until ctx ends: those
// already indexed when it starts, those added or changed later, and all of
// them again when a different embedding model is installed. Search uses
// whatever vectors exist, so it never waits for this.
func (s *Store) StartIndexing(ctx context.Context, admit Admit) {
	s.semMu.Lock()
	if s.kick != nil {
		s.semMu.Unlock()
		return
	}
	s.kick = make(chan struct{}, 1)
	s.semMu.Unlock()
	go s.indexLoop(ctx, admit)
	s.Kick()
}

// Kick asks the background indexer to look for passages without vectors,
// for example after an embedding model is installed. It does not wait.
func (s *Store) Kick() {
	s.semMu.Lock()
	kick := s.kick
	s.semMu.Unlock()
	if kick == nil {
		return
	}
	select {
	case kick <- struct{}{}:
	default:
	}
}

func (s *Store) indexLoop(ctx context.Context, admit Admit) {
	var retry <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.kick:
		case <-retry:
		}
		retry = nil
		if err := s.EmbedPending(ctx, admit); err != nil && ctx.Err() == nil {
			retry = time.After(indexRetry)
		}
	}
}

// pendingChunk is a passage without a vector from the current model.
type pendingChunk struct {
	rowid    int64
	sourceID string
	text     string
}

// EmbedPending embeds every passage that has no vector from the current
// embedding model. It returns nil when nothing is left or no embedding model
// is installed, and ErrNotNow when the model cannot run at the moment.
func (s *Store) EmbedPending(ctx context.Context, admit Admit) error {
	models := s.supportingModels()
	if models == nil {
		return nil
	}
	for {
		emb, err := models.Embedder(ctx)
		if err != nil || emb == nil {
			return err
		}
		page, err := s.pending(ctx, emb.ModelID(), pendingPage)
		if err != nil || len(page) == 0 {
			return err
		}
		stored := 0
		for start := 0; start < len(page); start += embedBatch {
			batch := page[start:min(start+embedBatch, len(page))]
			release := func() {}
			if admit != nil {
				if release, err = admit(ctx); err != nil {
					return err
				}
			}
			// The model may have been unloaded while this waited.
			if emb, err = models.Embedder(ctx); err != nil || emb == nil {
				release()
				return err
			}
			n, err := s.embedAndStore(ctx, emb, batch)
			release()
			if err != nil {
				return err
			}
			stored += n
		}
		// Nothing in a whole page could be stored (every passage was replaced
		// or embedded to nothing); stop rather than retry it forever.
		if stored == 0 {
			return nil
		}
	}
}

// pending lists passages from sources small enough to embed that lack a
// vector from model.
func (s *Store) pending(ctx context.Context, model string, limit int) ([]pendingChunk, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM knowledge_sources src
		WHERE status = ? AND chunk_count > 0 AND chunk_count <= ?
		  AND chunk_count > (SELECT COUNT(*) FROM knowledge_vectors v WHERE v.source_id = src.id AND v.model = ?)
		ORDER BY created_at`, StatusReady, maxEmbeddedChunks, model)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []pendingChunk
	for _, id := range ids {
		rows, err := s.db.QueryContext(ctx, `
			SELECT f.rowid, f.title, f.body FROM knowledge_fts f
			WHERE f.source_id = ?
			  AND f.rowid NOT IN (SELECT chunk_rowid FROM knowledge_vectors WHERE source_id = ? AND model = ?)
			LIMIT ?`, id, id, model, limit-len(out))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c pendingChunk
			var title, body string
			if err := rows.Scan(&c.rowid, &title, &body); err != nil {
				rows.Close()
				return nil, err
			}
			c.sourceID, c.text = id, embedText(title, body)
			out = append(out, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// embedAndStore embeds a batch and saves the vectors, returning how many it
// saved. A passage that a reindex replaced meanwhile is skipped; the next pass
// picks up its successor.
func (s *Store) embedAndStore(ctx context.Context, emb Embedder, batch []pendingChunk) (int, error) {
	texts := make([]string, len(batch))
	for i, c := range batch {
		texts[i] = c.text
	}
	vecs, err := emb.EmbedDocuments(ctx, texts)
	if err != nil {
		return 0, err
	}
	if len(vecs) != len(batch) {
		return 0, fmt.Errorf("the embedding model returned %d vectors for %d passages", len(vecs), len(batch))
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	stored := 0
	for i, c := range batch {
		var title, body string
		err := tx.QueryRowContext(ctx, `SELECT title, body FROM knowledge_fts WHERE rowid = ? AND source_id = ?`, c.rowid, c.sourceID).
			Scan(&title, &body)
		if err != nil || embedText(title, body) != c.text {
			continue
		}
		enc := encodeVector(vecs[i])
		if enc == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO knowledge_vectors (chunk_rowid, source_id, model, hash, vec) VALUES (?, ?, ?, ?, ?)`,
			c.rowid, c.sourceID, emb.ModelID(), textHash(c.text), enc); err != nil {
			return 0, err
		}
		stored++
	}
	return stored, tx.Commit()
}
