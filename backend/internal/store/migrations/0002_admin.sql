ALTER TABLE users
    ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT false;

-- At most one bootstrap administrator may exist. The unique partial index
-- makes concurrent startup or accidental privilege escalation fail loudly.
CREATE UNIQUE INDEX users_single_admin_key
    ON users (is_admin) WHERE is_admin;

-- Invitation-code registration has been replaced by administrator-managed
-- accounts. Existing households and ledger data are retained.
ALTER TABLE households
    DROP COLUMN invite_code;
