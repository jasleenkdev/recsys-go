-- 000008_seed_models.up.sql
INSERT INTO models (version, embedding_dim, purpose, trained_at, notes) VALUES
    ('mpnet-v1', 768, 'behavioral', now(), 'all-mpnet-base-v2, used for user/item embeddings'),
    ('mpnet-v1', 768, 'transcript_search', now(), 'all-mpnet-base-v2, used for transcript chunk embeddings');