package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	data := []byte("projects_dir: ~/Work\n" +
		"temp_dir: /tmp/custom\n" +
		"temp_ttl: 12h\n" +
		"terminal: iterm2\n")
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(Overrides{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Terminal != "iterm2" {
		t.Fatalf("expected terminal iterm2, got %s", cfg.Terminal)
	}
	if cfg.TempDir != "/tmp/custom" {
		t.Fatalf("expected temp dir /tmp/custom, got %s", cfg.TempDir)
	}
	if cfg.TempTTL != 12*time.Hour {
		t.Fatalf("expected temp ttl 12h, got %s", cfg.TempTTL)
	}
	if cfg.ProjectsDir == "~/Work" {
		t.Fatalf("expected projects dir to be expanded, got %s", cfg.ProjectsDir)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("PRT_PROJECTS_DIR", "/opt/projects")
	t.Setenv("PRT_TEMP_TTL", "2h")
	t.Setenv("PRT_TERMINAL", "terminal")

	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ProjectsDir != "/opt/projects" {
		t.Fatalf("expected projects dir /opt/projects, got %s", cfg.ProjectsDir)
	}
	if cfg.TempTTL != 2*time.Hour {
		t.Fatalf("expected temp ttl 2h, got %s", cfg.TempTTL)
	}
	if cfg.Terminal != "terminal" {
		t.Fatalf("expected terminal terminal, got %s", cfg.Terminal)
	}
}

func TestOverridesTakePrecedence(t *testing.T) {
	cfg, err := Load(Overrides{
		ProjectsDir: "/override",
		TempTTL:     "30m",
		Terminal:    "terminal",
		Verbose:     true,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ProjectsDir != "/override" {
		t.Fatalf("expected projects dir /override, got %s", cfg.ProjectsDir)
	}
	if cfg.TempTTL != 30*time.Minute {
		t.Fatalf("expected temp ttl 30m, got %s", cfg.TempTTL)
	}
	if cfg.Verbose != true {
		t.Fatalf("expected verbose true")
	}
}

func TestInvalidOverrideTTL(t *testing.T) {
	_, err := Load(Overrides{TempTTL: "nope"})
	if err == nil {
		t.Fatalf("expected error for invalid TTL")
	}
}
