-- Mimir semantic search (spec §61). When an embedding model is installed,
-- each passage gets a vector next to its full-text row. Vectors are keyed by
-- the passage's knowledge_fts rowid and deleted with it. model names the
-- embedding model, so vectors from another model are never compared, and hash
-- lets a reindex keep the vectors of passages whose text did not change.

CREATE TABLE IF NOT EXISTS knowledge_vectors (
    chunk_rowid INTEGER PRIMARY KEY,
    source_id TEXT NOT NULL,
    model TEXT NOT NULL,
    hash TEXT NOT NULL,
    vec BLOB NOT NULL
);

CREATE INDEX IF NOT EXISTS knowledge_vectors_source ON knowledge_vectors (source_id, model);
