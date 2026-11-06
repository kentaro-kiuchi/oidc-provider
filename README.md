# OIDC Provider

A self-hosted OpenID Connect provider.

## Development

```sh
cd provider
go run ./cmd/provider
```

The server listens on `:8080` by default. Set `OIDC_PROVIDER_ADDR` to change the address. `GET /healthz` returns the server health status.
