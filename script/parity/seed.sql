-- The rows the development database does not have, applied to both copies
-- before either app is started. Fixed timestamps, because "member since" is on
-- the page and a date computed at run time would differ between the two runs.
--
-- The digest is bcrypt(password123) at Rails' cost, so both apps authenticate
-- it: one hashing library, one format, one password for every user here.

-- Approved, not an admin, and with no start page at all: the empty grid, the
-- empty editor, and the redirect away from the admin section.
INSERT INTO users (email, password_digest, admin, approved, theme_preference, color_preference,
                   "columns", created_at, updated_at)
VALUES ('vacio@example.com', '$2a$12$nYYSn/08fEFyb/.cUcNRreAJUdKpRwyL9..7Bm1u1zOVGHbNs.Y/S',
        0, 1, 'dark', 'purple', 2, '2026-08-01 00:00:00', '2026-08-01 00:00:00');

-- Waiting for approval: the Approve toggle and the blocked badge on the admin
-- list, and the account the admin's password reset mail goes to.
INSERT INTO users (email, password_digest, admin, approved, theme_preference, color_preference,
                   "columns", created_at, updated_at)
VALUES ('pendiente@example.com', '$2a$12$nYYSn/08fEFyb/.cUcNRreAJUdKpRwyL9..7Bm1u1zOVGHbNs.Y/S',
        0, 0, 'system', 'teal', 1, '2026-08-02 00:00:00', '2026-08-02 00:00:00');

-- Signing in is part of the sequence, so every session in the copy is a
-- leftover from real use. Clearing them keeps the run from depending on them.
DELETE FROM sessions;
