package config

import (
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
	}
}

// Load reads the given YAML file, applies defaults for missing fields, and
// finally lets ROUTERHUB_HOST / ROUTERHUB_PORT environment variables override
// anything from the file. A missing file is not an error — defaults are used.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	// Re-apply defaults for zero values that may come from the YAML file.
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}

	// Environment variables have the highest priority (Docker friendly).
	if envHost := os.Getenv("ROUTERHUB_HOST"); envHost != "" {
		cfg.Server.Host = envHost
	}
	if envPort := os.Getenv("ROUTERHUB_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 && p < 65536 {
			cfg.Server.Port = p
		}
	}

	return cfg, nil
}
