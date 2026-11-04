-- +goose Up
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('student', 'teacher', 'admin'));
ALTER TABLE users DROP CONSTRAINT users_check;
ALTER TABLE users ADD CONSTRAINT users_role_group_check CHECK (
    (role = 'student' AND group_id IS NOT NULL) OR
    (role IN ('teacher', 'admin') AND group_id IS NULL)
);

CREATE TABLE setup_state (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    initialized_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

-- +goose Down
-- Adding the old constraint refuses a rollback while administrators exist.
-- Goose rolls back the entire migration; accounts are never silently deleted.
ALTER TABLE users DROP CONSTRAINT users_role_group_check;
ALTER TABLE users ADD CONSTRAINT users_check CHECK (
    (role = 'student' AND group_id IS NOT NULL) OR
    (role = 'teacher' AND group_id IS NULL)
);
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('student', 'teacher'));
DROP TABLE setup_state;
