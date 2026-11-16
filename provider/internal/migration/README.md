# Database migrations

This package is used only by `cmd/migrate`. It contains the migration runner
and embedded SQL assets; the provider API does not import it or apply schema
changes at startup.

Add schema changes as new `NNNNNN_description.up.sql` files. Each file contains
both `-- +goose Up` and `-- +goose Down` sections.
Once a migration has been applied in a shared environment, do not edit it;
add a new migration instead. `oidc-provider-migrate up` applies pending
migrations and takes a PostgreSQL advisory lock to prevent concurrent runners.

The task should use a dedicated database credential with schema-change
permissions. The API should use a separate runtime credential with only the
permissions it needs for application queries.

`cmd/migrate` opens a single-connection pool through `internal/database` and
passes it to this package. That shared package handles Secrets Manager retrieval,
TLS, and credential rotation for new connections. This package only runs the
migrations; it does not own or close the pool. See the repository README for
the environment variables and ECS task-role permissions.
