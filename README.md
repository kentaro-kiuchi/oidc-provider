# OIDC Provider

A self-hosted OpenID Connect provider.

## Development

```sh
cd provider
go run ./cmd/provider
```

The server listens on `:8080` by default. Set `OIDC_PROVIDER_ADDR` to change the address. `GET /healthz` returns the server health status.

## Database migrations

Versioned PostgreSQL migrations and their runner are separate from the API
implementation. SQL files are embedded in the migration executable; the API
does not load or apply them at startup.

Build the container image from the repository root. In an ECS standalone task,
override the container `command` with:

```json
["/usr/local/bin/oidc-provider-migrate", "up"]
```

For RDS, pass a Secrets Manager ARN and non-secret connection settings as
ordinary environment variables:

```text
DB_SECRET_ARN=arn:aws:secretsmanager:ap-northeast-1:123456789012:secret:oidc-migrate-AbCdEf
DB_HOST=your-database.ap-northeast-1.rds.amazonaws.com
DB_PORT=5432
DB_NAME=postgres
```

`DB_PORT` defaults to `5432`. `DB_HOST` and `DB_NAME` are required. Secret-based
connections always use TLS with certificate and hostname verification
(`sslmode=verify-full`). The Dockerfile downloads the AWS RDS global CA bundle
over HTTPS during the build, includes it at `/app/certs/rds-ca-bundle.pem`, and
sets `DB_SSLROOTCERT` to that path. ECS does not need to set this variable.
To use a different trusted bundle, provide the file and override
`DB_SSLROOTCERT`. When running outside the image, set this variable to the path
of your RDS CA bundle.

When AWS updates the CA bundle, rebuild the image with `docker build --no-cache`
and redeploy to refresh the downloaded file; a cached build layer does not
automatically detect changes at the download URL. Database password rotation
does not require rebuilding the image.

The secret must be a JSON `SecretString` with non-empty `username` and `password`
strings. Additional fields are ignored, so RDS-managed master secrets have a
compatible format, but use a dedicated migration user with schema-change
permissions rather than the master user. The future API should use a separate
runtime user and secret with only the permissions needed for its queries.

Credentials are fetched by the application, not injected using ECS `secrets`.
Grant the ECS **task role** `secretsmanager:GetSecretValue` on the specific ARN
and, for a customer-managed KMS key, the required `kms:Decrypt` permission.
The AWS SDK uses the standard credential chain (including ECS task credentials)
and the region in the secret ARN. The task needs network access to Secrets
Manager (via NAT or a VPC endpoint) and to the RDS endpoint on its database port.
Do not place database passwords or AWS access keys in task environment variables.

The shared `internal/database` package reads `AWSCURRENT` whenever the pool
opens a new physical PostgreSQL connection. It does not cache credentials
between connections, so rotation does not require restarting the task. Existing
authenticated sessions stay in the pool and can continue their work. If a new
connection fails with an authentication error, the package fetches the secret
again and retries connection establishment once. It never replays SQL or entire
migrations. Secret retrieval failures stop the new connection; old credentials
are not used as a fallback. Each connection therefore incurs a Secrets Manager
request (two if authentication is retried); ordinary queries on pooled sessions
do not. A later API implementation can reuse `database.Open` and the same pool
without importing migration code. The current API does not open a database.

For local development only, `DATABASE_URL` remains supported, for example:

```sh
DATABASE_URL='postgres://app:password@localhost:5432/oidc?sslmode=disable' \
  go run ./cmd/migrate up
```

`DATABASE_URL` is restricted to `localhost`, `127.0.0.1`, or `::1` and cannot be
combined with `DB_SECRET_ARN`. Remote connections must use the secret-based
settings above.

The command exits with status `0` when all pending migrations have been applied,
including when the schema is already current. `oidc-provider-migrate version`
prints the current schema version.
