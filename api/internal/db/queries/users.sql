-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name, is_site_admin)
VALUES (lower(sqlc.arg(email)), sqlc.arg(password_hash), sqlc.arg(display_name), sqlc.arg(is_site_admin))
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = lower(sqlc.arg(email));

-- name: GetUserByID :one
SELECT * FROM users WHERE id = sqlc.arg(id);

-- name: UpdateUserDisplayName :one
UPDATE users
SET display_name = sqlc.arg(display_name), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;
