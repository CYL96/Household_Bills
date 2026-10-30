-- Profile images live in PostgreSQL so deployments keep a single durable store.
-- Existing accounts start without an avatar and remain active (deleted_at NULL).
ALTER TABLE users
    ADD COLUMN avatar_data BYTEA,
    ADD COLUMN avatar_type TEXT,
    ADD COLUMN avatar_version INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN deleted_at TIMESTAMPTZ;

ALTER TABLE users
    ADD CONSTRAINT users_avatar_type_check
        CHECK (avatar_type IS NULL OR avatar_type IN ('image/png', 'image/jpeg')),
    ADD CONSTRAINT users_avatar_size_check
        CHECK (avatar_data IS NULL OR octet_length(avatar_data) BETWEEN 1 AND 2097152),
    ADD CONSTRAINT users_avatar_shape_check
        CHECK (
            (avatar_data IS NULL AND avatar_type IS NULL AND avatar_version = 0)
            OR (avatar_data IS NOT NULL AND avatar_type IS NOT NULL AND avatar_version >= 1)
        );
