package portal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// DefaultDelay is the minimum spacing between throttled portal requests.
const DefaultDelay = 700 * time.Millisecond

// Config holds user preferences for the data.go.kr integration.
type Config struct {
	// AutoApply, when true, lets `gongctl apply` submit without the y/n prompt.
	// Default false — applications are account-mutating, so confirmation is the
	// safe default; the user opts into automation explicitly.
	AutoApply bool `json:"autoApply"`
}

func configFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// LoadConfig reads the saved config, returning defaults if none exists.
func LoadConfig() (*Config, error) {
	path, err := configFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save writes the config 0600.
func (c *Config) Save() error {
	path, err := configFilePath()
	if err != nil {
		return err
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(path, data, 0o600)
}
