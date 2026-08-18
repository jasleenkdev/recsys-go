-- 000012_add_user_embeddings_unique.up.sql
ALTER TABLE user_embeddings
    ADD CONSTRAINT user_embeddings_user_model_unique UNIQUE (user_id, model_id);
