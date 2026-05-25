ALTER TABLE documents
  ADD COLUMN uploader_id   TEXT NOT NULL DEFAULT '',
  ADD COLUMN uploader_name TEXT NOT NULL DEFAULT '';
