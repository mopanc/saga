package saga

import (
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	HomeDir string
	DBPath  string
}

func LoadConfig() (*Config, error) {
	home := os.Getenv("SAGA_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("user home: %w", err)
		}
		home = filepath.Join(h, ".saga")
	}

	dbPath := os.Getenv("SAGA_DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(home, "index.db")
	}

	return &Config{
		HomeDir: home,
		DBPath:  dbPath,
	}, nil
}
