-- 000010_add_ingest_staging.up.sql

CREATE TABLE repo_ingest_staging (
    id BIGSERIAL PRIMARY KEY,
    github_id BIGINT NOT NULL UNIQUE,  -- GitHub's own repo ID, prevents re-fetching duplicates
    owner TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    stars INTEGER NOT NULL DEFAULT 0,
    topics TEXT[],
    language TEXT,
    embedded BOOLEAN NOT NULL DEFAULT false,
    item_id BIGINT REFERENCES items(id),  -- filled in once promoted to the real table
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE readme_chunk_staging (
    id BIGSERIAL PRIMARY KEY,
    staging_repo_id BIGINT NOT NULL REFERENCES repo_ingest_staging(id) ON DELETE CASCADE,
    section_heading TEXT,
    chunk_text TEXT NOT NULL,
    chunk_index INTEGER NOT NULL,
    embedded BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
