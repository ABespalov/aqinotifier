// Package dashboard handles loading layouts, managing visual endpoints,
// preparing telemetry and rendering dashboard screens to EPD, PNG, or BMP.
package dashboard

import (
	"fmt"

	"github.com/ABespalov/csirender"
	"github.com/rs/zerolog/log"
)

// Config represents the complete dashboard layout and endpoint configuration.
type Config struct {
	csirender.LayoutConfig `yaml:",inline"`

	Lang    string            `yaml:"lang"`    // Default language for localization
	Units   map[string]string `yaml:"units"`   // Standard measurements override map
	Allowed []string          `yaml:"allowed"` // Access Control List for the dashboard
}

var parser = csirender.NewParser[*Config]()

// ParseConfig loads a dashboard layout config from disk with caching.
func ParseConfig(path string) (*Config, error) {
	cfg, err := parser.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parsing dashboard config: %w", err)
	}

	log.Debug().Str("file", path).Msg("dashboard: loaded layout settings from file")
	return cfg, nil
}
