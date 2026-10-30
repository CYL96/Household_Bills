-- Account-to-account transfers are represented by a single row: account_id
-- is the source account and transfer_account_id is the destination. Keeping
-- both sides on one row means transfers never count as income or expense.
ALTER TABLE transactions
    DROP CONSTRAINT IF EXISTS transactions_kind_check;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_kind_check
    CHECK (kind IN ('income', 'expense', 'transfer'));

ALTER TABLE transactions
    ADD COLUMN transfer_account_id BIGINT;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_transfer_account_fk
    FOREIGN KEY (transfer_account_id, household_id)
    REFERENCES accounts (id, household_id) ON DELETE CASCADE;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_transfer_shape_check
    CHECK (
        (kind = 'transfer'
            AND transfer_account_id IS NOT NULL
            AND transfer_account_id <> account_id
            AND category = '账户转账')
        OR (kind <> 'transfer' AND transfer_account_id IS NULL)
    );

CREATE INDEX transactions_transfer_account_date_idx
    ON transactions (transfer_account_id, tx_date)
    WHERE transfer_account_id IS NOT NULL;
