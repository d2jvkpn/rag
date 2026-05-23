CREATE TABLE users (
  user_id UUID PRIMARY KEY DEFAULT uuidv7(),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  status TEXT NOT NULL,
  last_login_at TIMESTAMPTZ
);

CREATE TABLE documents (
  document_id UUID PRIMARY KEY DEFAULT uuidv7(),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  knowledge_base_id TEXT NOT NULL,
  filename TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  tags TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
  source_type TEXT NOT NULL,
  storage_path TEXT NOT NULL,
  chunk_snapshot_path TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL,
  status TEXT NOT NULL,
  stage TEXT NOT NULL,
  error_message TEXT NOT NULL DEFAULT '',
  retry_count INTEGER NOT NULL DEFAULT 0,
  page_count INTEGER NOT NULL DEFAULT 0,
  chunk_count INTEGER NOT NULL DEFAULT 0,
  chunk_version INTEGER NOT NULL DEFAULT 0,
  chunk_config_hash TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  UNIQUE (knowledge_base_id, sha256)
);

CREATE INDEX idx_documents_knowledge_base_id ON documents (knowledge_base_id);

CREATE TABLE document_chunks (
  chunk_id UUID PRIMARY KEY DEFAULT uuidv7(),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  document_id UUID NOT NULL REFERENCES documents(document_id),
  chunk_index INTEGER NOT NULL,
  section_title TEXT NOT NULL DEFAULT '',
  page_start INTEGER NOT NULL DEFAULT 0,
  page_end INTEGER NOT NULL DEFAULT 0,
  text TEXT NOT NULL,
  normalized_text TEXT NOT NULL,
  status TEXT NOT NULL,
  chunk_version INTEGER NOT NULL,
  source TEXT NOT NULL,
  is_current BOOLEAN NOT NULL DEFAULT TRUE,
  review_comment TEXT NOT NULL DEFAULT '',
  filename TEXT NOT NULL,
  embedding_model TEXT NOT NULL DEFAULT '',
  resource_refs JSONB NOT NULL DEFAULT '[]'::jsonb
);

CREATE INDEX idx_document_chunks_document_id ON document_chunks (document_id);
CREATE INDEX idx_document_chunks_document_id_chunk_index ON document_chunks (document_id, chunk_index);
