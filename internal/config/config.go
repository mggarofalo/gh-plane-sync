// Package config handles YAML configuration parsing and validation for
// gh-plane-sync. It supports multiple repo-to-project mappings with global
// default state mappings and optional per-mapping overrides.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPath is the default path to the config file.
const DefaultConfigPath = "/etc/gh-plane-sync/config.yaml"

// DefaultDBPath is the default path to the SQLite database.
const DefaultDBPath = "/var/lib/gh-plane-sync/sync.db"

// DefaultInterval is the default sync interval when running in daemon mode.
const DefaultInterval = 30 * time.Minute

// MinInterval is the minimum allowed sync interval.
const MinInterval = 1 * time.Minute

// Config is the top-level configuration for gh-plane-sync.
type Config struct {
	Plane    PlaneConfig   `yaml:"plane"`
	States   StateMappings `yaml:"states"`
	Mappings []Mapping     `yaml:"mappings"`
	DBPath   string        `yaml:"db_path"`
	Interval Duration      `yaml:"interval"`
}

// Duration wraps time.Duration to support YAML unmarshalling from a Go
// duration string (e.g. "30m", "1h30m", "90s").
type Duration struct {
	time.Duration
}

// UnmarshalYAML parses a YAML string as a Go time.Duration.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Value == "" {
		return nil
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("parsing duration %q: %w", value.Value, err)
	}
	d.Duration = parsed
	return nil
}

// PlaneConfig holds the Plane instance connection details.
type PlaneConfig struct {
	APIURL    string `yaml:"api_url"`
	Workspace string `yaml:"workspace"`
}

// StateMappings defines bidirectional state mappings between GitHub and Plane.
type StateMappings struct {
	GitHubToPlane map[string]string `yaml:"github_to_plane"`
	PlaneToGitHub map[string]string `yaml:"plane_to_github"`
}

// Mapping associates one GitHub repository with one Plane project.
// If States is nil, the global default state mappings apply.
type Mapping struct {
	GitHub GitHubRepo     `yaml:"github"`
	Plane  PlaneProject   `yaml:"plane"`
	States *StateMappings `yaml:"states,omitempty"`
}

// GitHubRepo identifies a GitHub repository.
type GitHubRepo struct {
	Owner string `yaml:"owner"`
	Repo  string `yaml:"repo"`
}

// PlaneProject identifies a Plane project.
type PlaneProject struct {
	ProjectID string `yaml:"project_id"`
}

// EffectiveStates returns the state mappings for this mapping. If the mapping
// has per-mapping overrides, those are returned; otherwise the provided global
// defaults are returned.
func (m *Mapping) EffectiveStates(global StateMappings) StateMappings {
	if m.States != nil {
		return *m.States
	}
	return global
}

// Load reads the YAML config file at the given path, parses it into a Config
// struct, applies defaults, and validates required fields.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // config path is intentionally user-supplied via CLI flag
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if cfg.DBPath == "" {
		cfg.DBPath = DefaultDBPath
	}

	if cfg.Interval.Duration == 0 {
		cfg.Interval.Duration = DefaultInterval
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// validate checks that all required fields are present and that there are no
// duplicate repository entries.
func (c *Config) validate() error {
	if c.Interval.Duration < MinInterval {
		return fmt.Errorf("interval %s is below minimum %s", c.Interval.Duration, MinInterval)
	}

	if c.Plane.APIURL == "" {
		return fmt.Errorf("plane.api_url is required")
	}
	if c.Plane.Workspace == "" {
		return fmt.Errorf("plane.workspace is required")
	}

	if len(c.States.GitHubToPlane) == 0 {
		return fmt.Errorf("states.github_to_plane is required")
	}
	if len(c.States.PlaneToGitHub) == 0 {
		return fmt.Errorf("states.plane_to_github is required")
	}

	if len(c.Mappings) == 0 {
		return fmt.Errorf("at least one mapping is required")
	}

	seen := make(map[string]bool)
	for i, m := range c.Mappings {
		if m.GitHub.Owner == "" {
			return fmt.Errorf("mappings[%d].github.owner is required", i)
		}
		if m.GitHub.Repo == "" {
			return fmt.Errorf("mappings[%d].github.repo is required", i)
		}
		if m.Plane.ProjectID == "" {
			return fmt.Errorf("mappings[%d].plane.project_id is required", i)
		}

		key := m.GitHub.Owner + "/" + m.GitHub.Repo
		if seen[key] {
			return fmt.Errorf("duplicate mapping for repository %s", key)
		}
		seen[key] = true

		if m.States != nil {
			if len(m.States.GitHubToPlane) == 0 {
				return fmt.Errorf("mappings[%d].states.github_to_plane must not be empty when states is specified", i)
			}
			if len(m.States.PlaneToGitHub) == 0 {
				return fmt.Errorf("mappings[%d].states.plane_to_github must not be empty when states is specified", i)
			}
		}
	}

	return nil
}
