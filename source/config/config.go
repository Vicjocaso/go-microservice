package config

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// StorageConfig holds storage-related configurations
type StorageConfig struct {
	AWSRegion            string
	AWSAccessKeyID       string
	AWSSecretAccessKey   string
	GCPProjectID         string
	GCPCredentialsFile   string
	DefaultCloud         string // e.g., "aws", "gcp"
	ReplicateToAllClouds bool   // Whether to replicate uploads to all configured clouds
}

// DatabaseConfig holds database-related configurations
type DatabaseConfig struct {
	Username string
	Password string
	Host     string
	Port     uint16
	Name     string
	SslMode  string
}

// AppConfig holds application-wide configurations
type AppConfig struct {
	ServerPort    uint16
	Database      DatabaseConfig
	BucketName    string
	GotenbergURL  string
	StorageConfig StorageConfig
}

// applyDatabaseURL parses a postgres/postgresql URL (including Railway's DATABASE_URL)
// into DatabaseConfig. Query parameters such as sslmode are honored when present.
func applyDatabaseURL(cfg *AppConfig, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse database URL: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("unsupported database URL scheme %q (expected postgres)", u.Scheme)
	}
	if u.User != nil {
		cfg.Database.Username = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			cfg.Database.Password = pw
		}
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("database URL missing host")
	}
	cfg.Database.Host = host
	if portStr := u.Port(); portStr != "" {
		port, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return fmt.Errorf("invalid database port: %w", err)
		}
		cfg.Database.Port = uint16(port)
	} else {
		cfg.Database.Port = 5432
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return fmt.Errorf("database URL missing database name")
	}
	if i := strings.Index(dbName, "/"); i >= 0 {
		dbName = dbName[:i]
	}
	cfg.Database.Name = dbName
	if ssl := u.Query().Get("sslmode"); ssl != "" {
		cfg.Database.SslMode = ssl
	}
	return nil
}

func LoadConfig() *AppConfig {
	cfg := AppConfig{
		GotenbergURL: "http://localhost:3001",
		Database: DatabaseConfig{
			Name:     "equilibria_files",
			Port:     5432,
			Username: "postgres",
			Password: "password",
			Host:     "localhost",
			SslMode:  "disable",
		},
		ServerPort: 3000,
		BucketName: "test-file-manager-2025",
		StorageConfig: StorageConfig{
			AWSRegion:            "us-east-1",
			AWSAccessKeyID:       "",
			AWSSecretAccessKey:   "",
			GCPProjectID:         "",
			GCPCredentialsFile:   "",
			DefaultCloud:         "aws",
			ReplicateToAllClouds: false,
		},
	}

	if secrets, exists := os.LookupEnv("SECRETS"); exists {
		secretsMap := make(map[string]string)
		err := json.Unmarshal([]byte(secrets), &secretsMap)
		if err != nil {
			log.Fatalf("Error parsing secrets: %v", err)
		}

		if dbURL, exists := secretsMap["DATABASE_URL"]; exists {
			if err := applyDatabaseURL(&cfg, dbURL); err != nil {
				log.Fatalf("Invalid DATABASE_URL in SECRETS: %v", err)
			}
		}

		if bucketName, exists := secretsMap["BUCKET_NAME"]; exists {
			cfg.BucketName = bucketName
		}

		if AWSRegion, exists := secretsMap["AWS_REGION"]; exists {
			cfg.StorageConfig.AWSRegion = AWSRegion
		}

		if AWSAccessKeyID, exists := secretsMap["AWS_ACCESS_KEY_ID"]; exists {
			cfg.StorageConfig.AWSAccessKeyID = AWSAccessKeyID
		}

		if AWSSecretAccessKey, exists := secretsMap["AWS_SECRET_ACCESS_KEY"]; exists {
			cfg.StorageConfig.AWSSecretAccessKey = AWSSecretAccessKey
		}

		if GCPProjectID, exists := secretsMap["GCP_PROJECT_ID"]; exists {
			cfg.StorageConfig.GCPProjectID = GCPProjectID
		}
		if GCPCredentialsFile, exists := secretsMap["GCP_CREDENTIALS_FILE"]; exists {
			cfg.StorageConfig.GCPCredentialsFile = GCPCredentialsFile
		}

		if DefaultCloud, exists := secretsMap["DEFAULT_CLOUD"]; exists {
			cfg.StorageConfig.DefaultCloud = DefaultCloud
		}

		if ReplicateToAllClouds, exists := secretsMap["REPLICATE_TO_ALL_CLOUDS"]; exists {
			cfg.StorageConfig.ReplicateToAllClouds = ReplicateToAllClouds == "true"
		}

		if GotenbergURL, exists := secretsMap["GOTENBERG_URL"]; exists {
			cfg.GotenbergURL = GotenbergURL
		}
	}

	// Railway and other hosts inject DATABASE_URL directly when Postgres is attached.
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		if err := applyDatabaseURL(&cfg, dbURL); err != nil {
			log.Fatalf("Invalid DATABASE_URL: %v", err)
		}
	}

	if bucket := os.Getenv("BUCKET_NAME"); bucket != "" {
		cfg.BucketName = bucket
	}
	if cloud := os.Getenv("DEFAULT_CLOUD"); cloud != "" {
		cfg.StorageConfig.DefaultCloud = cloud
	}

	// Prefer empty-env checks so JSON SECRETS values are not wiped when variables are unset.
	if v := os.Getenv("AWS_REGION"); v != "" {
		cfg.StorageConfig.AWSRegion = v
	}
	if v := os.Getenv("AWS_ACCESS_KEY_ID"); v != "" {
		cfg.StorageConfig.AWSAccessKeyID = v
	}
	if v := os.Getenv("AWS_SECRET_ACCESS_KEY"); v != "" {
		cfg.StorageConfig.AWSSecretAccessKey = v
	}

	if gotenbergEnv := os.Getenv("GOTENBERG_URL"); gotenbergEnv != "" {
		cfg.GotenbergURL = gotenbergEnv
	}

	// Railway sets PORT; SERVER_PORT is supported for local .env parity.
	if p := os.Getenv("PORT"); p != "" {
		if parsed, err := strconv.ParseUint(p, 10, 16); err == nil {
			cfg.ServerPort = uint16(parsed)
		} else {
			log.Fatalf("Invalid PORT %q: %v", p, err)
		}
	} else if p := os.Getenv("SERVER_PORT"); p != "" {
		if parsed, err := strconv.ParseUint(p, 10, 16); err == nil {
			cfg.ServerPort = uint16(parsed)
		} else {
			log.Fatalf("Invalid SERVER_PORT %q: %v", p, err)
		}
	}

	return &cfg
}

func (c *AppConfig) LoadDbUri() string {
	db := c.Database
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(db.Username, db.Password),
		Host:   net.JoinHostPort(db.Host, strconv.FormatUint(uint64(db.Port), 10)),
		Path:   "/" + db.Name,
	}
	if db.SslMode != "" {
		u.RawQuery = url.Values{"sslmode": []string{db.SslMode}}.Encode()
	}
	return u.String()
}
