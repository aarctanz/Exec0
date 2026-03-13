-- name: CreateLanguage :one
INSERT INTO languages (
    name,
    version,
    source_file,
    compile_command,
    run_command
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: GetLanguageByID :one
SELECT id, name, version, source_file, compile_command, run_command FROM languages
WHERE id = $1
LIMIT 1;

-- name: ListLanguages :many
SELECT id, name, version, source_file, compile_command, run_command FROM languages
ORDER BY id;

-- name: ListActiveLanguages :many
SELECT id, name, version, source_file, compile_command, run_command FROM languages
WHERE is_archived = FALSE
ORDER BY id;

-- name: ListPublicLanguages :many
SELECT id, name, version, is_archived
FROM languages
ORDER BY name;

-- name: GetPublicLanguageByID :one
SELECT id, name, version, is_archived
FROM languages
WHERE id = $1
LIMIT 1;
