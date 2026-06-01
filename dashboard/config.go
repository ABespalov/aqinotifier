// Package dashboard handles loading layouts, managing visual endpoints,
// preparing telemetry and rendering dashboard screens to EPD, PNG, or BMP.
package dashboard

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Position defines the 2D coordinate of a visual element on the screen (in pixels).
type Position struct {
	X float64 `yaml:"x"`
	Y float64 `yaml:"y"`
}

// Size defines the dimensions of a visual container (in pixels).
type Size struct {
	W float64 `yaml:"w"`
	H float64 `yaml:"h"`
}

// FontStyle groups font family reference and foreground/background colors.
type FontStyle struct {
	Face string `yaml:"face"`
	Fg   string `yaml:"fg"`
	Bg   string `yaml:"bg"`
}

// Align defines vertical, horizontal, and relative alignments, along with spacers.
type Align struct {
	H string  `yaml:"h"` // Horizontal alignment: left, center, right
	V string  `yaml:"v"` // Vertical alignment: top, center, bottom, under_center, under
	P string  `yaml:"p"` // Placement relative to other elements: right, left, bottom, none
	S float64 `yaml:"s"` // Spacing/gap size in pixels
}

// Margin defines external spacing around components.
type Margin struct {
	T float64 `yaml:"t"` // Top margin
	B float64 `yaml:"b"` // Bottom margin
	L float64 `yaml:"l"` // Left margin
	R float64 `yaml:"r"` // Right margin
}

// ThresholdLine configures a horizontal threshold line drawn on charts.
type ThresholdLine struct {
	Condition string  `yaml:"condition"` // Expression to trigger the line (e.g. "zone == 2")
	Value     float64 `yaml:"value"`     // Static threshold value (automatically resolved if condition matches zone)
	Color     string  `yaml:"color"`     // Hex or named color reference
	Fg        string  `yaml:"fg"`        // Foreground color override
	Bg        string  `yaml:"bg"`        // Background color override
	W         float64 `yaml:"w"`         // Line width/thickness
	Style     string  `yaml:"style"`     // Dash pattern (e.g. "dashed.2.14")
}

// Threshold defines a condition-based styling override.
type Threshold struct {
	Condition string `yaml:"condition"` // Comparison expression (e.g. "aqi.us.zone.index >= 3")
	Label     string `yaml:"label"`     // Custom text label on match
	Bg        string `yaml:"bg"`        // Custom background color on match
	Fg        string `yaml:"fg"`        // Custom foreground/text color on match
	Color     string `yaml:"color"`     // Hex color code reference
}

// BorderConfig controls the border line and background of card components.
type BorderConfig struct {
	Thin       float64     `yaml:"thin"`       // Border thickness in pixels (0 for no line)
	Fg         string      `yaml:"fg"`         // Border stroke color
	Bg         string      `yaml:"bg"`         // Background fill color
	Padding    float64     `yaml:"padding"`    // Padding inside the border
	Affects    []string    `yaml:"affects"`    // Elements enclosed by the border (e.g. ["value", "unit"])
	Thresholds []Threshold `yaml:"thresholds"` // Dynamic styling overrides based on thresholds
}

// ComponentConfig configures sub-components inside a complex widget like a value card.
type ComponentConfig struct {
	Font       FontStyle     `yaml:"font"`       // Font settings
	Align      Align         `yaml:"align"`      // Alignment settings
	Margin     Margin        `yaml:"margin"`     // Outer spacing
	Thresholds []Threshold   `yaml:"thresholds"` // Color thresholds
	Border     *BorderConfig `yaml:"border"`     // Optional border settings
	Template   string        `yaml:"template"`   // Visual formatting template
	Parameter  string        `yaml:"parameter"`  // Custom unit reference
}

// Element is a generic node representing a visual item inside a layout.
type Element struct {
	Type           string                 `yaml:"type"`            // Element type: text, value, line, chart
	Position       Position               `yaml:"position"`        // Offset position on canvas
	Size           Size                   `yaml:"size"`            // Layout box dimensions
	Radius         float64                `yaml:"radius"`          // Border corner rounding radius
	Source         string                 `yaml:"source"`          // Data mapping path (e.g. "pm25.value")
	Parameters     map[string]string      `yaml:"parameters"`      // Arbitrary key-value parameters
	Value          ComponentConfig        `yaml:"value"`           // Value sub-component configuration
	Label          ComponentConfig        `yaml:"label"`           // Label sub-component configuration
	Unit           ComponentConfig        `yaml:"unit"`            // Unit sub-component configuration
	Border         *BorderConfig          `yaml:"border"`          // Border configuration
	Font           FontStyle              `yaml:"font"`            // Font settings for basic elements
	Align          Align                  `yaml:"align"`           // Basic alignment options
	Padding        float64                `yaml:"padding"`         // Insets inside container
	FitText        *bool                  `yaml:"fit_text"`        // Toggle automatic text scaling
	Format         string                 `yaml:"format"`          // Formatting string (e.g. "%.0f")
	Color          string                 `yaml:"color"`           // Color reference
	Template       string                 `yaml:"template"`        // String template for text elements
	ChartType      string                 `yaml:"chart_type"`      // Obsolete chart property
	Style          string                 `yaml:"style"`           // Chart style: bar, line
	Duration       string                 `yaml:"duration"`        // Chart history range (e.g. "24h")
	Points         int                    `yaml:"points"`          // Number of columns/points in chart
	Axis           map[string]interface{} `yaml:"axis"`            // Axis configurations (X and Y parameters)
	ThresholdLines []ThresholdLine        `yaml:"threshold_lines"` // Dashed guidelines on charts
	Thresholds     []Threshold            `yaml:"thresholds"`      // Value-based thresholds
}

// Config represents the complete dashboard layout configuration.
type Config struct {
	Screen struct {
		Width        int               `yaml:"width"`         // Canvas width in pixels
		Height       int               `yaml:"height"`        // Canvas height in pixels
		AntiAliasing bool              `yaml:"anti_aliasing"` // Enable/disable anti-aliasing
		Palette      map[string]string `yaml:"palette"`       // Logical palette color mappings
	} `yaml:"screen"`
	Defaults struct {
		FitText *bool `yaml:"fit_text"` // Global fit_text default setting
		Align   Align `yaml:"align"`    // Global text alignment defaults
	} `yaml:"defaults"`
	Output struct {
		Format     string           `yaml:"format"`     // Serialization format: epd_raw, png, bmp
		Background string           `yaml:"background"` // Base fill color
		Mapping    map[string][]int `yaml:"mapping"`    // Bit packing map for EPD outputs
	} `yaml:"output"`
	Lang    string            `yaml:"lang"`    // Default language for localization
	Units   map[string]string `yaml:"units"`   // Standard measurements override map
	Allowed []string          `yaml:"allowed"` // Access Control List for the dashboard
	Fonts   map[string]struct {
		File    string  `yaml:"file"`    // Path to TrueType font file
		Size    float64 `yaml:"size"`    // Default size of font
		Hinting string  `yaml:"hinting"` // Hinting mode: none, vertical, full
	} `yaml:"fonts"`
	Layout []Element `yaml:"layout"` // Chronological sequence of elements to draw
}

// ParseConfig loads a dashboard layout YAML config from disk.
func ParseConfig(path string) (*Config, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading dashboard config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return nil, fmt.Errorf("parsing dashboard YAML: %w", err)
	}

	return &cfg, nil
}
