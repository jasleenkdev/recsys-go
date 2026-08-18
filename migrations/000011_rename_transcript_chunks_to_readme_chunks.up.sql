ALTER TABLE transcript_chunks RENAME TO readme_chunks;

ALTER TABLE readme_chunks
    DROP COLUMN timestamp_start,
    DROP COLUMN timestamp_end;

ALTER TABLE readme_chunks
    ADD COLUMN section_heading TEXT,
    ADD COLUMN chunk_index INTEGER NOT NULL DEFAULT 0;