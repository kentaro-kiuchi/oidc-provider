// Package database provides PostgreSQL connections for both migrations and the API.
package database

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/jackc/pgx/v5"
)

// Config contains a secret reference and non-secret connection settings.
// URL is supported only for local development, without a SecretARN.
type Config struct {
	SecretARN string
	Host      string
	Port      string
	Name      string
	RootCert  string
	URL       string
}

func ConfigFromEnv() Config {
	return Config{
		SecretARN: os.Getenv("DB_SECRET_ARN"),
		Host:      os.Getenv("DB_HOST"),
		Port:      os.Getenv("DB_PORT"),
		Name:      os.Getenv("DB_NAME"),
		RootCert:  os.Getenv("DB_SSLROOTCERT"),
		URL:       os.Getenv("DATABASE_URL"),
	}
}

func (c Config) connectionConfig() (*pgx.ConnConfig, error) {
	if c.SecretARN == "" {
		u, err := url.Parse(c.URL)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || !localHost(u.Hostname()) {
			return nil, errors.New("set DB_SECRET_ARN and DB_HOST/DB_NAME; DATABASE_URL is allowed only for localhost development")
		}
		cfg, err := pgx.ParseConfig(c.URL)
		if err != nil {
			// Parse errors may contain the complete URL, including its password.
			return nil, errors.New("invalid local DATABASE_URL")
		}
		// Prevent URL parameters from overriding the localhost restriction.
		if !localHost(cfg.Host) {
			return nil, errors.New("DATABASE_URL must connect to localhost")
		}
		for _, fallback := range cfg.Fallbacks {
			if !localHost(fallback.Host) {
				return nil, errors.New("DATABASE_URL must connect to localhost")
			}
		}
		return cfg, nil
	}
	if c.URL != "" {
		return nil, errors.New("DB_SECRET_ARN and DATABASE_URL cannot be combined")
	}
	secret, err := arn.Parse(c.SecretARN)
	if err != nil || secret.Service != "secretsmanager" || secret.Region == "" || secret.AccountID == "" || !strings.HasPrefix(secret.Resource, "secret:") || len(secret.Resource) <= len("secret:") {
		return nil, errors.New("DB_SECRET_ARN must be a Secrets Manager secret ARN")
	}
	if strings.TrimSpace(c.Host) == "" || strings.TrimSpace(c.Name) == "" {
		return nil, errors.New("DB_HOST and DB_NAME are required with DB_SECRET_ARN")
	}
	port := c.Port
	if port == "" {
		port = "5432"
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return nil, errors.New("DB_PORT must be between 1 and 65535")
	}
	u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.Host, port), Path: "/" + c.Name}
	q := url.Values{"sslmode": {"verify-full"}, "connect_timeout": {"10"}}
	if c.RootCert != "" {
		q.Set("sslrootcert", c.RootCert)
	}
	u.RawQuery = q.Encode()
	cfg, err := pgx.ParseConfig(u.String())
	if err != nil {
		return nil, errors.New("invalid PostgreSQL connection settings; check DB_HOST/DB_NAME/DB_SSLROOTCERT")
	}
	return cfg, nil
}

func localHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
