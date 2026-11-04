-- +goose Up
CREATE UNIQUE INDEX subjects_name_normalized_key ON subjects (lower(btrim(name)));

-- +goose Down
DROP INDEX subjects_name_normalized_key;
