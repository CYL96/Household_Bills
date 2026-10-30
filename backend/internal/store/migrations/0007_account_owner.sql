-- A wallet may optionally belong to one household member. A NULL owner marks a
-- wallet shared by the whole household; ownership is only a label, never access
-- control, so every member still reads and writes every account.
ALTER TABLE accounts
    ADD COLUMN owner_user_id BIGINT;

-- The composite foreign key keeps the owner inside the account's household and
-- reuses users_id_household_key. Users are soft-deleted, so no ON DELETE action
-- is needed: ledger history and owner labels keep resolving after a member
-- leaves. No lookup index is added because accounts are never filtered by owner.
ALTER TABLE accounts
    ADD CONSTRAINT accounts_owner_fk
        FOREIGN KEY (owner_user_id, household_id)
        REFERENCES users (id, household_id);
