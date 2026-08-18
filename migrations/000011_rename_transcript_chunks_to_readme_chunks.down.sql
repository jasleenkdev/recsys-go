ALTER TABLE readme_chunks
    DROP COLUMN section_heading,
    DROP COLUMN chunk_index;

ALTER TABLE readme_chunks
    ADD COLUMN timestamp_start INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN timestamp_end INTEGER NOT NULL DEFAULT 0;

ALTER TABLE readme_chunks RENAME TO transcript_chunks;