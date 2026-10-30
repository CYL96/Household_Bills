-- Store transaction dates with second precision. Existing calendar dates
-- become midnight of the same day without losing information.
ALTER TABLE transactions
    ALTER COLUMN tx_date TYPE TIMESTAMP(0) WITHOUT TIME ZONE
    USING CAST(tx_date AS TIMESTAMP(0) WITHOUT TIME ZONE);
