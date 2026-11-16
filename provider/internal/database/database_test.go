package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const testARN = "arn:aws:secretsmanager:ap-northeast-1:123456789012:secret:database-AbCdEf"

type secretFunc func(context.Context, *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error)

func (f secretFunc) GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return f(ctx, in)
}

type connectorFunc func(context.Context) (driver.Conn, error)

func (f connectorFunc) Connect(ctx context.Context) (driver.Conn, error) { return f(ctx) }
func (f connectorFunc) Driver() driver.Driver                            { return nil }

type testConn struct{ driver.Conn }

func (testConn) Close() error                       { return nil }
func (testConn) Ping(context.Context) error         { return nil }
func (testConn) ResetSession(context.Context) error { return nil }

func baseConfig(t *testing.T) *pgx.ConnConfig {
	t.Helper()
	cfg, err := pgx.ParseConfig("postgres://localhost/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestPoolReadsCurrentCredentialsForEachNewConnection(t *testing.T) {
	var mu sync.Mutex
	password := "old"
	fetches := 0
	var used []string
	c := &rotatingConnector{
		config: baseConfig(t), secretARN: testARN,
		client: secretFunc(func(ctx context.Context, in *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
			mu.Lock()
			defer mu.Unlock()
			if aws.ToString(in.SecretId) != testARN || aws.ToString(in.VersionStage) != "AWSCURRENT" {
				t.Error("must request AWSCURRENT from the configured ARN")
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Error("secret request must have a deadline")
			}
			fetches++
			return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(`{"username":"app","password":"` + password + `"}`)}, nil
		}),
		newConnector: func(cfg *pgx.ConnConfig) driver.Connector {
			mu.Lock()
			used = append(used, cfg.User+":"+cfg.Password)
			mu.Unlock()
			return connectorFunc(func(context.Context) (driver.Conn, error) { return testConn{}, nil })
		},
	}
	db := sql.OpenDB(c)
	defer db.Close()
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	password = "rotated"
	mu.Unlock()
	// Reusing an authenticated connection must not fetch credentials again.
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	// Holding the original session forces the pool to create a new connection.
	second, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	mu.Lock()
	defer mu.Unlock()
	if fetches != 2 || strings.Join(used, ",") != "app:old,app:rotated" {
		t.Fatalf("fetches=%d, credentials=%v", fetches, used)
	}
}

func TestConnectRetryIsLimitedToAuthenticationFailures(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		attempts   int
	}{
		{"password rotation", "28P01", 2},
		{"authorization rotation", "28000", 2},
		{"database missing", "3D000", 1},
		{"network failure", "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetches, connects := 0, 0
			c := &rotatingConnector{
				config: baseConfig(t), secretARN: testARN,
				client: secretFunc(func(context.Context, *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
					fetches++
					return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(`{"username":"app","password":"valid"}`)}, nil
				}),
				newConnector: func(*pgx.ConnConfig) driver.Connector {
					return connectorFunc(func(context.Context) (driver.Conn, error) {
						connects++
						if tc.code == "" {
							return nil, errors.New("network failure")
						}
						return nil, &pgconn.PgError{Code: tc.code}
					})
				},
			}
			if _, err := c.Connect(context.Background()); err == nil {
				t.Fatal("expected connection failure")
			}
			if fetches != tc.attempts || connects != tc.attempts {
				t.Fatalf("fetches=%d, connects=%d", fetches, connects)
			}
		})
	}
}

func TestRotationBetweenFetchAndLoginRecovers(t *testing.T) {
	fetches := 0
	var used []string
	cfg := baseConfig(t)
	originalUser, originalPassword := cfg.User, cfg.Password
	c := &rotatingConnector{
		config: cfg, secretARN: testARN,
		client: secretFunc(func(context.Context, *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
			fetches++
			payload := `{"username":"old-user","password":"old"}`
			if fetches > 1 {
				payload = `{"username":"new-user","password":"new:@/?#% password"}`
			}
			return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(payload)}, nil
		}),
		newConnector: func(current *pgx.ConnConfig) driver.Connector {
			used = append(used, current.User+":"+current.Password)
			return connectorFunc(func(context.Context) (driver.Conn, error) {
				if current.Password == "old" {
					return nil, &pgconn.PgError{Code: "28P01"}
				}
				return testConn{}, nil
			})
		},
	}
	conn, err := c.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if fetches != 2 || strings.Join(used, ",") != "old-user:old,new-user:new:@/?#% password" {
		t.Fatalf("rotation was not applied: fetches=%d, credentials=%v", fetches, used)
	}
	if cfg.User != originalUser || cfg.Password != originalPassword {
		t.Fatal("shared base configuration was mutated")
	}
}

func TestInvalidSecretDoesNotConnectOrExposePayload(t *testing.T) {
	for _, payload := range []string{"sensitive-invalid-json", `{"username":"app"}`, `{"password":"sensitive"}`, `{"username":123,"password":"sensitive"}`} {
		c := &rotatingConnector{
			config: baseConfig(t), secretARN: testARN,
			client: secretFunc(func(context.Context, *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
				return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(payload)}, nil
			}),
			newConnector: func(*pgx.ConnConfig) driver.Connector {
				t.Fatal("must not connect with invalid credentials")
				return nil
			},
		}
		_, err := c.Connect(context.Background())
		if err == nil || strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("unsafe or missing error: %v", err)
		}
	}
}

func TestSecretFailureStopsConnection(t *testing.T) {
	want := errors.New("access denied")
	c := &rotatingConnector{
		config: baseConfig(t), secretARN: testARN,
		client: secretFunc(func(context.Context, *secretsmanager.GetSecretValueInput) (*secretsmanager.GetSecretValueOutput, error) {
			return nil, want
		}),
		newConnector: func(*pgx.ConnConfig) driver.Connector { t.Fatal("must not connect after secret failure"); return nil },
	}
	if _, err := c.Connect(context.Background()); !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestConnectionConfig(t *testing.T) {
	for _, key := range []string{"PGHOST", "PGHOSTADDR", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE", "PGSSLMODE", "PGSSLROOTCERT", "PGSERVICE", "PGSERVICEFILE"} {
		t.Setenv(key, "")
	}
	for _, tc := range []struct {
		name  string
		cfg   Config
		valid bool
	}{
		{"missing settings", Config{}, false},
		{"secret", Config{SecretARN: testARN, Host: "db.example.com", Name: "postgres"}, true},
		{"missing host", Config{SecretARN: testARN, Name: "postgres"}, false},
		{"invalid ARN", Config{SecretARN: "secret-name", Host: "db.example.com", Name: "postgres"}, false},
		{"ambiguous settings", Config{SecretARN: testARN, URL: "postgres://localhost/test"}, false},
		{"invalid port", Config{SecretARN: testARN, Host: "db.example.com", Name: "postgres", Port: "65536"}, false},
		{"local", Config{URL: "postgres://app:sensitive@localhost/test?sslmode=disable"}, true},
		{"remote URL", Config{URL: "postgres://app:sensitive@db.example.com/test?sslmode=verify-full"}, false},
		{"host override", Config{URL: "postgres://localhost/test?host=db.example.com"}, false},
		{"host fallback", Config{URL: "postgres://localhost/test?host=localhost,db.example.com"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := tc.cfg.connectionConfig()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if err != nil && strings.Contains(err.Error(), "sensitive") {
				t.Fatal("error exposed password")
			}
			if err == nil && tc.cfg.SecretARN != "" {
				if cfg.TLSConfig == nil || cfg.TLSConfig.InsecureSkipVerify || cfg.TLSConfig.ServerName != "db.example.com" || len(cfg.Fallbacks) != 0 {
					t.Fatal("must enforce TLS hostname verification without plaintext fallbacks")
				}
			}
		})
	}
}
