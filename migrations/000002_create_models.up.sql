CREATE TABLE models (
    id BIGSERIAL PRIMARY KEY,
    version TEXT NOT NULL,
    purpose TEXT NOT NULL
        CHECK (purpose IN ('behavioral', 'transcript_search')),
    embedding_dim INTEGER NOT NULL,
    trained_at TIMESTAMPTZ NOT NULL,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (purpose, version)
);
