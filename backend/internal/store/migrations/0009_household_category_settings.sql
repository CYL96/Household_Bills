-- Categories are a household-wide snapshot shared by every member. Both kinds
-- live in the same row so a single PUT atomically replaces the complete
-- configuration. revision is the optimistic-concurrency token; it starts at 1
-- and increments on every successful save.
CREATE TABLE household_category_settings (
    household_id BIGINT PRIMARY KEY
        REFERENCES households (id) ON DELETE CASCADE,
    revision BIGINT NOT NULL CHECK (revision >= 1),
    expense JSONB NOT NULL CHECK (jsonb_typeof(expense) = 'object'),
    income JSONB NOT NULL CHECK (jsonb_typeof(income) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
