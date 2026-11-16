FROM debian:trixie-slim AS rds-certs
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /certs \
    && curl --fail --silent --show-error --location --retry 3 \
        --connect-timeout 10 --max-time 60 \
        https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem \
        --output /certs/rds-ca-bundle.pem \
    && test -s /certs/rds-ca-bundle.pem \
    && chmod 0644 /certs/rds-ca-bundle.pem

FROM golang:1.27.3-trixie AS provider-build
WORKDIR /src/provider
COPY provider/go.mod provider/go.sum ./
RUN go mod download
COPY provider/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/oidc-provider ./cmd/provider \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/oidc-provider-migrate ./cmd/migrate

FROM debian:trixie-slim AS runtime
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 app \
    && useradd --system --uid 10001 --gid app --home-dir /app app \
    && mkdir -p /app/data \
    && chown -R 10001:10001 /app
WORKDIR /app
COPY --from=provider-build --chown=10001:10001 /out/oidc-provider /usr/local/bin/oidc-provider
COPY --from=provider-build --chown=10001:10001 /out/oidc-provider-migrate /usr/local/bin/oidc-provider-migrate
COPY --from=rds-certs /certs/rds-ca-bundle.pem /app/certs/rds-ca-bundle.pem
ENV OIDC_PROVIDER_ADDR=:8080
ENV DB_SSLROOTCERT=/app/certs/rds-ca-bundle.pem
EXPOSE 8080
USER 10001:10001
CMD ["/usr/local/bin/oidc-provider"]
