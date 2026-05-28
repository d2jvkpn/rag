CREATE TABLE knowledge_bases (
  knowledge_base_id TEXT PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  dim INTEGER NOT NULL,
  analyzer TEXT NOT NULL DEFAULT 'chinese',
  chunk_size INTEGER NOT NULL DEFAULT 1000,
  chunk_overlap INTEGER NOT NULL DEFAULT 150,
  min_chunks INTEGER NOT NULL DEFAULT 3
);
