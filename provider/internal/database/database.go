package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

type secretClient interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// Open creates a shared database/sql pool. Every new physical connection reads
// AWSCURRENT; existing authenticated sessions remain usable during rotation.
// It does not ping the database. The caller must close the pool.
func Open(ctx context.Context, c Config) (*sql.DB, error) {
	cfg, err := c.connectionConfig()
	if err != nil {
		return nil, err
	}
	var connector driver.Connector = stdlib.GetConnector(*cfg)
	if c.SecretARN != "" {
		secretARN, _ := arn.Parse(c.SecretARN) // Already validated by connectionConfig.
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(secretARN.Region))
		if err != nil {
			return nil, fmt.Errorf("load AWS configuration: %w", err)
		}
		connector = &rotatingConnector{
			config: cfg, secretARN: c.SecretARN,
			client:       secretsmanager.NewFromConfig(awsCfg),
			newConnector: func(cfg *pgx.ConnConfig) driver.Connector { return stdlib.GetConnector(*cfg) },
		}
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}

type rotatingConnector struct {
	config       *pgx.ConnConfig
	secretARN    string
	client       secretClient
	newConnector func(*pgx.ConnConfig) driver.Connector
}

func (c *rotatingConnector) Driver() driver.Driver { return stdlib.GetDefaultDriver() }

func (c *rotatingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	// Bound both credential retrieval and connection establishment, including
	// when database/sql calls Connect with a long-lived context.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		cfg, err := c.currentConfig(ctx)
		if err != nil {
			return nil, err
		}
		conn, err := c.newConnector(cfg).Connect(ctx)
		var pgErr *pgconn.PgError
		if attempt == 0 && errors.As(err, &pgErr) && (pgErr.Code == "28P01" || pgErr.Code == "28000") {
			// Rotation may have changed the password between fetch and login.
			// Retry only connection establishment, never SQL or migrations.
			continue
		}
		return conn, err
	}
	panic("unreachable")
}

func (c *rotatingConnector) currentConfig(ctx context.Context) (*pgx.ConnConfig, error) {
	secret, err := c.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(c.secretARN), VersionStage: aws.String("AWSCURRENT"),
	})
	if err != nil {
		return nil, fmt.Errorf("retrieve database secret: %w", err)
	}
	if secret == nil || secret.SecretString == nil {
		return nil, errors.New("database secret must contain a JSON SecretString")
	}
	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal([]byte(*secret.SecretString), &credentials); err != nil {
		return nil, errors.New("database secret must contain valid JSON credentials")
	}
	if credentials.Username == "" || credentials.Password == "" {
		return nil, errors.New("database secret requires non-empty username and password")
	}
	cfg := c.config.Copy()
	cfg.User, cfg.Password = credentials.Username, credentials.Password
	return cfg, nil
}
