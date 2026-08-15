ALTER TABLE users
    ADD CONSTRAINT fk_users_active_embedding
    FOREIGN KEY (active_embedding_id)
    REFERENCES user_embeddings(id)
    ON DELETE SET NULL;

ALTER TABLE items
    ADD CONSTRAINT fk_items_active_embedding
    FOREIGN KEY (active_embedding_id)
    REFERENCES item_embeddings(id)
    ON DELETE SET NULL;
