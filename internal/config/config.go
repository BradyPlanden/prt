package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultProjectsDir = "~/Projects"
	defaultTempDir     = "/tmp/prt"
	defaultTempTTL     = 24 * time.Hour
	defaultTerminal    = "auto"
	defaultConfigPath  = "~/.config/prt/config.yaml"
)

// Config stores runtime settings for repository and terminal behavior.
type Config struct {
	ProjectsDir string
	TempDir     string
	TempTTL     time.Duration
	Terminal    string
	Verbose     bool
}

// Overrides contains CLI-supplied values that override file and env config.
type Overrides struct {
	ProjectsDir string
	TempDir     string
	TempTTL     string
	Terminal    string
	Verbose     bool
	ConfigPath  string
}

type fileConfig struct {
	ProjectsDir string `yaml:"projects_dir"`
	TempDir     string `yaml:"temp_dir"`
	TempTTL     string `yaml:"temp_ttl"`
	Terminal    string `yaml:"terminal"`
}

// Load reads configuration from disk, environment, and explicit overrides.
func Load(overrides Overrides) (Config, error) {
	cfg := Config{
		ProjectsDir: defaultProjectsDir,
		TempDir:     defaultTempDir,
		TempTTL:     defaultTempTTL,
		Terminal:    defaultTerminal,
		Verbose:     false,
	}

	configPath := overrides.ConfigPath
	if configPath == "" {
		configPath = defaultConfigPath
	}

	expandedConfigPath, err := expandPath(configPath)
	if err != nil {
		return Config{}, err
	}

	if err := applyFileConfig(&cfg, expandedConfigPath); err != nil {
		return Config{}, err
	}

	applyEnv(&cfg)
	if err := applyOverrides(&cfg, overrides); err != nil {
		return Config{}, err
	}

	if err := expandConfigPaths(&cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func applyFileConfig(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config: %w", err)
	}

	var fileCfg fileConfig
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if fileCfg.ProjectsDir != "" {
		cfg.ProjectsDir = fileCfg.ProjectsDir
	}
	if fileCfg.TempDir != "" {
		cfg.TempDir = fileCfg.TempDir
	}
	if fileCfg.TempTTL != "" {
		parsed, err := time.ParseDuration(fileCfg.TempTTL)
		if err != nil {
			return fmt.Errorf("invalid temp_ttl: %w", err)
		}
		cfg.TempTTL = parsed
	}
	if fileCfg.Terminal != "" {
		cfg.Terminal = fileCfg.Terminal
	}

	return nil
}

func applyEnv(cfg *Config) {
	if value := os.Getenv("PRT_PROJECTS_DIR"); value != "" {
		cfg.ProjectsDir = value
	}
	if value := os.Getenv("PRT_TEMP_DIR"); value != "" {
		cfg.TempDir = value
	}
	if value := os.Getenv("PRT_TEMP_TTL"); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			cfg.TempTTL = parsed
		}
	}
	if value := os.Getenv("PRT_TERMINAL"); value != "" {
		cfg.Terminal = value
	}
	if value := os.Getenv("PRT_VERBOSE"); value != "" {
		cfg.Verbose = parseBool(value)
	}
}

func applyOverrides(cfg *Config, overrides Overrides) error {
	if overrides.ProjectsDir != "" {
		cfg.ProjectsDir = overrides.ProjectsDir
	}
	if overrides.TempDir != "" {
		cfg.TempDir = overrides.TempDir
	}
	if overrides.TempTTL != "" {
		parsed, err := time.ParseDuration(overrides.TempTTL)
		if err != nil {
			return fmt.Errorf("invalid temp_ttl override: %w", err)
		}
		cfg.TempTTL = parsed
	}
	if overrides.Terminal != "" {
		cfg.Terminal = overrides.Terminal
	}
	if overrides.Verbose {
		cfg.Verbose = true
	}

	return nil
}

func expandConfigPaths(cfg *Config) error {
	var err error
	cfg.ProjectsDir, err = expandPath(cfg.ProjectsDir)
	if err != nil {
		return err
	}
	cfg.TempDir, err = expandPath(cfg.TempDir)
	if err != nil {
		return err
	}
	return nil
}

func expandPath(path string) (string, error) {
	if path == "" {
		return path, nil
	}

	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		if path == "~" {
			path = home
		} else if strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		}
	}

	return filepath.Clean(path), nil
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
