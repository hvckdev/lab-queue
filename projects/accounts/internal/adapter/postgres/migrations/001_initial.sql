-- +goose Up
-- IF NOT EXISTS adopts databases created by the original embedded migrator.
-- Existing users and the legacy accounts_schema_migrations table are retained.
CREATE TABLE IF NOT EXISTS student_groups (
    id BIGINT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE CHECK (char_length(name) BETWEEN 1 AND 100)
);

CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    email TEXT NOT NULL UNIQUE CHECK (email = lower(btrim(email))),
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('student', 'teacher')),
    group_id BIGINT REFERENCES student_groups(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CHECK ((role = 'student' AND group_id IS NOT NULL) OR
           (role = 'teacher' AND group_id IS NULL))
);

-- +goose Down
DROP TABLE users;
DROP TABLE student_groups;
