package saga

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	HomeDir           string
	DBPath            string
	BaselineMaxTokens int
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

	baselineMax := DefaultBaselineMaxTokens
	if v := os.Getenv("SAGA_BASELINE_MAX_TOKENS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			baselineMax = parsed
		}
	}

	return &Config{
		HomeDir:           home,
		DBPath:            dbPath,
		BaselineMaxTokens: baselineMax,
	}, nil
}
