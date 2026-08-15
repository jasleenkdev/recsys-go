ALTER TABLE items
    DROP CONSTRAINT fk_items_active_embedding;

ALTER TABLE users
    DROP CONSTRAINT fk_users_active_embedding;
