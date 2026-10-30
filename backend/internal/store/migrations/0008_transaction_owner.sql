-- Transactions carry two distinct member references: created_by records who
-- entered the row, while owner_user_id records whom the money belongs to. They
-- are usually the same but bookkeeping on behalf of another member makes them
-- differ. Existing rows are attributed to their recorder, so the column can be
-- mandatory and every transaction always has exactly one owner.
ALTER TABLE transactions
    ADD COLUMN owner_user_id BIGINT;

UPDATE transactions
SET owner_user_id = created_by;

ALTER TABLE transactions
    ALTER COLUMN owner_user_id SET NOT NULL;

-- The composite foreign key keeps the owner inside the transaction's household
-- and reuses users_id_household_key. Users are soft-deleted, so no ON DELETE
-- action is needed: ledger history and attribution keep resolving after a
-- member leaves. No lookup index is added because transactions are never
-- filtered or grouped by owner.
ALTER TABLE transactions
    ADD CONSTRAINT transactions_owner_fk
        FOREIGN KEY (owner_user_id, household_id)
        REFERENCES users (id, household_id);
