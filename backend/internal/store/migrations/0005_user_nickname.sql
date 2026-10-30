-- Optional display nickname, independent of the unique login username. A blank
-- nickname falls back to the username wherever a display name is shown.
ALTER TABLE users
    ADD COLUMN nickname TEXT NOT NULL DEFAULT ''
    CHECK (char_length(nickname) <= 50);
