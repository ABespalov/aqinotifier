// Package config defines the configuration structures, default configurations,
// and file loading functions for the AQI Notifier Bot.
// It handles parsing and unmarshalling of yaml configuration files (e.g. aqinotifier.yaml),
// path resolution relative to the application binary, and loading secret tokens from files.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Server holds HTTP server binding settings and the URL path used by the
// application to receive POST requests from sensors.
type Server struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Url      string `yaml:"url"`
	Protocol string `yaml:"protocol"`
	File     struct {
		Cert string `yaml:"cert"`
		Key  string `yaml:"key"`
	} `yaml:"file"`
	Timeout ServerTimeout `yaml:"timeout"`
}

// ServerTimeout groups timeout settings (in seconds) used by the HTTP server.
// Typical usage is to apply these values to net/http.Server.ReadTimeout,
// WriteTimeout and IdleTimeout.
type ServerTimeout struct {
	Server   int `yaml:"server"`
	Read     int `yaml:"read"`
	Write    int `yaml:"write"`
	Idle     int `yaml:"idle"`
	Shutdown int `yaml:"shutdown"`
}

// Log rotation settings
type LogRotate struct {
	Enabled    bool `yaml:"enabled"`
	MaxSizeMB  int  `yaml:"max_size_mb"`
	MaxBackups int  `yaml:"max_backups"`
	MaxAgeDays int  `yaml:"max_age_days"`
	Compress   bool `yaml:"compress"`
}

// Log holds local logging configuration (for zerolog/lumberjack).
type Log struct {
	Level   string    `yaml:"level"`
	LogFile string    `yaml:"log_file"`
	Format  string    `yaml:"format"`
	Rotate  LogRotate `yaml:"rotate"`
}

// NewLogConfig returns a Log pre-populated with sensible defaults.
// These defaults enable JSON output with file rotation enabled.
func NewLogConfig() *Log {
	app := getAppName()
	return &Log{
		Level:   "info",
		LogFile: app + ".log",
		Format:  "json",
		Rotate: LogRotate{
			Enabled:    false,
			MaxSizeMB:  20,
			MaxBackups: 1,
			MaxAgeDays: 30,
			Compress:   true,
		},
	}
}

// NewServerConfig returns a Server populated with sensible defaults.
func NewServerConfig() *Server {
	return &Server{
		Host:     "0.0.0.0",
		Port:     28288,
		Url:      "/aqi",
		Protocol: "https",
		File: struct {
			Cert string `yaml:"cert"`
			Key  string `yaml:"key"`
		}{
			Cert: "localhost.pem",
			Key:  "localhost-key.pem",
		},
		Timeout: ServerTimeout{
			Server:   30,
			Read:     15,
			Write:    15,
			Idle:     5,
			Shutdown: 5,
		},
	}
}

// Node returns the host:port component that should be used when dialing or
// displaying the server address. If the Host is a loopback or wildcard
// address, the host portion is left empty to produce ":port" which is
// valid for binding.
func (s Server) Node() string {
	host := strings.TrimSpace(s.Host)
	if host == "localhost" || host == "0.0.0.0" || host == "127.0.0.1" {
		host = ""
	}
	return fmt.Sprintf("%s:%d", host, s.Port)
}

// String returns the full server address including the URL path, e.g.
// ":8088/aqi" or "hostname:8088/aqi" depending on the Host value.
func (s Server) String() string {
	return fmt.Sprintf("%s%s", s.Node(), s.Url)
}

// Database contains settings required to open a connection to the SQL
// database and tune the connection pool.
type Database struct {
	Use  []string `yaml:"use"`
	File struct {
		Json  string `yaml:"json"`
		Pgsql string `yaml:"pgsql"`
	} `yaml:"file"`
	MaxValues       int    `yaml:"max_values"`
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	Db              string `yaml:"db"`
	User            string `yaml:"user"`
	Password        string `yaml:"password"`
	SslMode         string `yaml:"sslmode"`
	MaxOpenConns    int    `yaml:"max_open_conns"`
	MaxIdleConns    int    `yaml:"max_idle_conns"`
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"`
	Connections     struct {
		Retry int `yaml:"retry"`
		Delay int `yaml:"delay"`
	} `yaml:"connections"`
}

// HasUse returns true if the specified mode is present in Use configuration.
func (d Database) HasUse(mode string) bool {
	for _, u := range d.Use {
		if strings.EqualFold(u, mode) {
			return true
		}
	}
	return false
}

// DBProvider returns the database provider string if configured (any value in Use other than "json").
func (d Database) DBProvider() string {
	for _, u := range d.Use {
		if !strings.EqualFold(u, "json") {
			return u
		}
	}
	return ""
}

// NewDatabaseConfig returns a Database populated with conservative defaults
// suitable for local development. Adjust SslMode and pool sizes for
// production environments.
func NewDatabaseConfig() *Database {
	app := getAppName()
	return &Database{
		Use: []string{"postgres", "json"},
		File: struct {
			Json  string `yaml:"json"`
			Pgsql string `yaml:"pgsql"`
		}{
			Json:  app + ".data.json",
			Pgsql: app + ".pgsql",
		},
		MaxValues:       1500,
		Host:            "localhost",
		Port:            5432,
		Db:              "postgres",
		User:            "postgres",
		Password:        "",
		SslMode:         "disable",
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 300,
		Connections: struct {
			Retry int `yaml:"retry"`
			Delay int `yaml:"delay"`
		}{
			Retry: 3,
			Delay: 2,
		},
	}
}

// String returns a DSN-like representation of the database settings.
// Note: it embeds the password in the output — avoid logging this in
// production. Use this primarily for diagnostics in trusted contexts.
func (d Database) String() string {
	provider := d.DBProvider()
	if provider == "" {
		return "json"
	}
	return fmt.Sprintf("%s://%s:%s@%s:%d/%s", provider, d.User, d.Password, d.Host, d.Port, d.Db)
}

// System holds application-level operational settings that control in-memory
// caching and configuration hot-reload behaviour.
type System struct {
	// ValuesInRam is the maximum number of measurements kept in memory per device.
	// Older values are dropped; they are still queryable from the persistent store.
	ValuesInRam int `yaml:"values_in_ram"`
	// ConfigReloadTime is the polling interval (in seconds) for detecting changes
	// in the config file and related resource files. Set to 0 to disable reloading.
	ConfigReloadTime int `yaml:"config_reload_time"`
	// HealthCheckTime is the polling interval (in seconds) for database health checks.
	HealthCheckTime int `yaml:"health_check_time"`
}

// NewSystemConfig returns a System populated with sensible defaults.
func NewSystemConfig() *System {
	return &System{
		ValuesInRam:      10,
		ConfigReloadTime: 5,
		HealthCheckTime:  10,
	}
}

// MonitorLevelGroup defines the configuration for PM10 and PM25 thresholds.
type MonitorLevelGroup struct {
	Level1     float64 `yaml:"level1" json:"level1"`
	Level2     float64 `yaml:"level2" json:"level2"`
	Diff       float64 `yaml:"diff" json:"diff"`
	LazyNotify struct {
		Up   *int `yaml:"up" json:"up"`
		Down *int `yaml:"down" json:"down"`
	} `yaml:"lazy_notify" json:"lazy_notify"`
}

// MonitorAQIGroup defines the configuration for AQI notifications and lazy updates.
type MonitorAQIGroup struct {
	Standard   string `yaml:"standard" json:"standard"`
	LazyNotify struct {
		Up   *int `yaml:"up" json:"up"`
		Down *int `yaml:"down" json:"down"`
	} `yaml:"lazy_notify" json:"lazy_notify"`
}

// NotificationMap maps a category to a list of its configurations.
// During YAML unmarshalling, it converts nested level and direction blocks to items with "_up" and "_down" suffixes.
type NotificationMap map[string][]string

// mapLevelDirToCanonical maps shorthand or legacy level and direction strings to their canonical representations (e.g. "l1" -> "level1").
func mapLevelDirToCanonical(lvl, dir string) string {
	lvl = strings.ToLower(strings.TrimSpace(lvl))
	dir = strings.ToLower(strings.TrimSpace(dir))

	// Normalize level prefix
	switch lvl {
	case "l1":
		lvl = "level1"
	case "l2":
		lvl = "level2"
	case "l3":
		lvl = "level3"
	}

	// Normalize direction
	switch dir {
	case "u":
		dir = "up"
	case "d":
		dir = "down"
	}

	// Boundary-based transition mapping:
	// - level2 up (entering level 2 from below) -> level2_up (l2u)
	// - level2 down (leaving level 2 to level 1) -> level1_down (l1d)
	// - level3 up (entering level 3 from below) -> level3_up (l3u)
	// - level3 down (leaving level 3 to level 2) -> level2_down (l2d)
	// - level1 down (legacy fallback) -> level1_down (l1d)
	if lvl == "level2" && dir == "up" {
		return "level2_up"
	} else if lvl == "level2" && dir == "down" {
		return "level1_down"
	} else if lvl == "level3" && dir == "up" {
		return "level3_up"
	} else if lvl == "level3" && dir == "down" {
		return "level2_down"
	} else if lvl == "level1" && dir == "down" {
		return "level1_down"
	}

	return fmt.Sprintf("%s_%s", lvl, dir)
}

// appendUnique appends an item to a slice if it is not already present.
func appendUnique(slice []string, val string) []string {
	for _, v := range slice {
		if v == val {
			return slice
		}
	}
	return append(slice, val)
}

func (n *NotificationMap) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var raw map[string]interface{}
	if err := unmarshal(&raw); err != nil {
		return err
	}
	*n = make(NotificationMap)
	for cat, val := range raw {
		switch v := val.(type) {
		case []interface{}:
			var items []string
			for _, itemRaw := range v {
				switch item := itemRaw.(type) {
				case string:
					items = appendUnique(items, item)
				case map[interface{}]interface{}:
					for lvlRaw, dirsRaw := range item {
						lvlStr, okLvl := lvlRaw.(string)
						if !okLvl {
							continue
						}
						dirsSlice, okDirs := dirsRaw.([]interface{})
						if !okDirs {
							continue
						}
						for _, dirRaw := range dirsSlice {
							dirStr, okDir := dirRaw.(string)
							if !okDir {
								continue
							}
							canonical := mapLevelDirToCanonical(lvlStr, dirStr)
							items = appendUnique(items, canonical)
						}
					}
				case map[string]interface{}:
					for lvlStr, dirsRaw := range item {
						dirsSlice, okDirs := dirsRaw.([]interface{})
						if !okDirs {
							continue
						}
						for _, dirRaw := range dirsSlice {
							dirStr, okDir := dirRaw.(string)
							if !okDir {
								continue
							}
							canonical := mapLevelDirToCanonical(lvlStr, dirStr)
							items = appendUnique(items, canonical)
						}
					}
				}
			}
			(*n)[cat] = items
		}
	}
	return nil
}

type Monitor struct {
	PM10          MonitorLevelGroup            `yaml:"pm10" json:"pm10"`
	PM25          MonitorLevelGroup            `yaml:"pm25" json:"pm25"`
	AQI           MonitorAQIGroup              `yaml:"aqi" json:"aqi"`
	Notifications NotificationMap              `yaml:"notifications" json:"notifications"`
	Warnings      NotificationMap              `yaml:"warnings" json:"warnings"`
	DeviceNames   map[string]string            `yaml:"device_names" json:"device_names"`
	Corrections   map[string]map[string]string `yaml:"corrections" json:"corrections"`
}

// NewMonitorConfig returns a Monitor pre-populated with default values
func NewMonitorConfig() *Monitor {
	lazyUp := 2
	lazyDown := 3
	return &Monitor{
		PM10: MonitorLevelGroup{
			Level1: 54.0,
			Level2: 154.0,
			Diff:   66.0,
			LazyNotify: struct {
				Up   *int `yaml:"up" json:"up"`
				Down *int `yaml:"down" json:"down"`
			}{Up: &lazyUp, Down: &lazyDown},
		},
		PM25: MonitorLevelGroup{
			Level1: 9.0,
			Level2: 35.0,
			Diff:   50.0,
			LazyNotify: struct {
				Up   *int `yaml:"up" json:"up"`
				Down *int `yaml:"down" json:"down"`
			}{Up: &lazyUp, Down: &lazyDown},
		},
		AQI: MonitorAQIGroup{
			Standard: "EU",
			LazyNotify: struct {
				Up   *int `yaml:"up" json:"up"`
				Down *int `yaml:"down" json:"down"`
			}{Up: &lazyUp, Down: &lazyDown},
		},
		Notifications: NotificationMap{
			"aqi":  {"level1", "level2", "level3", "level4", "level5", "level6", "level7"},
			"vals": {"level3_up", "level1_down"},
		},
		Warnings: NotificationMap{
			"aqi":   {"level1", "level2", "level3", "level4", "level5", "level6", "level7"},
			"val25": {"level2_up", "level3_up", "level2_down", "level1_down"},
			"val10": {"level2_up", "level3_up", "level2_down", "level1_down"},
			"vals":  {"level2_up", "level3_up", "level2_down", "level1_down"},
		},
		DeviceNames: make(map[string]string),
		Corrections: make(map[string]map[string]string),
	}
}

var normalizeRe = regexp.MustCompile(`^(diff(?:10|25|s)?_)?(?:l|level)(\d+)(?:_?)(u|up|d|down)?$`)

// normalizeNotificationItem uses regex to parse and reconstruct a notification string into its canonical format.
func normalizeNotificationItem(item string) string {
	item = strings.ToLower(strings.TrimSpace(item))
	matches := normalizeRe.FindStringSubmatch(item)
	if matches == nil {
		return item
	}

	prefix := matches[1]
	lvl := matches[2]
	dir := matches[3]

	res := prefix + "level" + lvl
	if dir != "" {
		if strings.HasPrefix(dir, "u") {
			res += "_up"
		} else if strings.HasPrefix(dir, "d") {
			res += "_down"
		}
	}
	return res
}

// Validate ensures that the Monitor configuration is valid and populates defaults for missing values
func (m *Monitor) Validate() {
	defaults := NewMonitorConfig()
	if m.PM10.Level1 == 0 {
		m.PM10.Level1 = defaults.PM10.Level1
	}
	if m.PM25.Level1 == 0 {
		m.PM25.Level1 = defaults.PM25.Level1
	}
	if m.PM10.Level2 == 0 {
		m.PM10.Level2 = defaults.PM10.Level2
	}
	if m.PM25.Level2 == 0 {
		m.PM25.Level2 = defaults.PM25.Level2
	}
	if m.PM10.Diff == 0 {
		m.PM10.Diff = defaults.PM10.Diff
	}
	if m.PM25.Diff == 0 {
		m.PM25.Diff = defaults.PM25.Diff
	}
	if m.AQI.Standard == "" {
		m.AQI.Standard = defaults.AQI.Standard
	}
	if m.Notifications == nil {
		m.Notifications = defaults.Notifications
	} else {
		for cat, items := range m.Notifications {
			for i, item := range items {
				m.Notifications[cat][i] = normalizeNotificationItem(item)
			}
		}
	}
	if m.Warnings == nil {
		m.Warnings = defaults.Warnings
	} else {
		for cat, items := range m.Warnings {
			for i, item := range items {
				m.Warnings[cat][i] = normalizeNotificationItem(item)
			}
		}
	}
	if m.Corrections == nil {
		m.Corrections = defaults.Corrections
	}
	if m.AQI.LazyNotify.Up == nil {
		m.AQI.LazyNotify.Up = defaults.AQI.LazyNotify.Up
	}
	if m.AQI.LazyNotify.Down == nil {
		m.AQI.LazyNotify.Down = defaults.AQI.LazyNotify.Down
	}
	if m.PM25.LazyNotify.Up == nil {
		m.PM25.LazyNotify.Up = defaults.PM25.LazyNotify.Up
	}
	if m.PM25.LazyNotify.Down == nil {
		m.PM25.LazyNotify.Down = defaults.PM25.LazyNotify.Down
	}
	if m.PM10.LazyNotify.Up == nil {
		m.PM10.LazyNotify.Up = defaults.PM10.LazyNotify.Up
	}
	if m.PM10.LazyNotify.Down == nil {
		m.PM10.LazyNotify.Down = defaults.PM10.LazyNotify.Down
	}
}

// FlattenNotifications converts the nested map format back into the flat format expected by the app.
func FlattenNotifications(n NotificationMap) []string {
	var result []string
	for cat, items := range n {
		for _, item := range items {
			item = strings.ReplaceAll(item, "level", "l")
			item = strings.ReplaceAll(item, "_up", "u")
			item = strings.ReplaceAll(item, "up", "u")
			item = strings.ReplaceAll(item, "_down", "d")
			item = strings.ReplaceAll(item, "down", "d")
			result = append(result, fmt.Sprintf("%s_%s", cat, item))
		}
	}
	return result
}

// unflattenNotifications converts a flat slice of notification keys into a structured NotificationMap grouped by categories.
func unflattenNotifications(flat []string) NotificationMap {
	result := make(NotificationMap)
	for _, f := range flat {
		parts := strings.SplitN(f, "_", 2)
		if len(parts) == 2 {
			cat := parts[0]
			item := parts[1]
			result[cat] = append(result[cat], normalizeNotificationItem(item))
		}
	}
	return result
}

// UnmarshalJSON migrates legacy flat JSON settings into the new nested Monitor structure.
func (m *Monitor) UnmarshalJSON(data []byte) error {
	type Alias Monitor
	aux := &struct {
		*Alias
		PM10L1       float64         `json:"pm10_l1"`
		PM25L1       float64         `json:"pm25_l1"`
		PM10L2       float64         `json:"pm10_l2"`
		PM25L2       float64         `json:"pm25_l2"`
		PM10Diff     float64         `json:"pm10_diff"`
		PM25Diff     float64         `json:"pm25_diff"`
		AQIStandard  string          `json:"aqi_standard"`
		AQILazyUp    *int            `json:"aqi_lazy_up"`
		AQILazyDown  *int            `json:"aqi_lazy_down"`
		PM25LazyUp   *int            `json:"pm25_lazy_up"`
		PM25LazyDown *int            `json:"pm25_lazy_down"`
		PM10LazyUp   *int            `json:"pm10_lazy_up"`
		PM10LazyDown *int            `json:"pm10_lazy_down"`
		RawNotif     json.RawMessage `json:"notifications"`
		RawWarn      json.RawMessage `json:"warnings"`
	}{
		Alias: (*Alias)(m),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if m.PM10.Level1 == 0 && aux.PM10L1 != 0 {
		m.PM10.Level1 = aux.PM10L1
	}
	if m.PM25.Level1 == 0 && aux.PM25L1 != 0 {
		m.PM25.Level1 = aux.PM25L1
	}
	if m.PM10.Level2 == 0 && aux.PM10L2 != 0 {
		m.PM10.Level2 = aux.PM10L2
	}
	if m.PM25.Level2 == 0 && aux.PM25L2 != 0 {
		m.PM25.Level2 = aux.PM25L2
	}
	if m.PM10.Diff == 0 && aux.PM10Diff != 0 {
		m.PM10.Diff = aux.PM10Diff
	}
	if m.PM25.Diff == 0 && aux.PM25Diff != 0 {
		m.PM25.Diff = aux.PM25Diff
	}
	if m.AQI.Standard == "" && aux.AQIStandard != "" {
		m.AQI.Standard = aux.AQIStandard
	}
	if m.AQI.LazyNotify.Up == nil && aux.AQILazyUp != nil {
		m.AQI.LazyNotify.Up = aux.AQILazyUp
	}
	if m.AQI.LazyNotify.Down == nil && aux.AQILazyDown != nil {
		m.AQI.LazyNotify.Down = aux.AQILazyDown
	}
	if m.PM25.LazyNotify.Up == nil && aux.PM25LazyUp != nil {
		m.PM25.LazyNotify.Up = aux.PM25LazyUp
	}
	if m.PM25.LazyNotify.Down == nil && aux.PM25LazyDown != nil {
		m.PM25.LazyNotify.Down = aux.PM25LazyDown
	}
	if m.PM10.LazyNotify.Up == nil && aux.PM10LazyUp != nil {
		m.PM10.LazyNotify.Up = aux.PM10LazyUp
	}
	if m.PM10.LazyNotify.Down == nil && aux.PM10LazyDown != nil {
		m.PM10.LazyNotify.Down = aux.PM10LazyDown
	}

	// Process Notifications
	if len(aux.RawNotif) > 0 {
		var oldArray []string
		if err := json.Unmarshal(aux.RawNotif, &oldArray); err == nil {
			m.Notifications = unflattenNotifications(oldArray)
		} else {
			var newMap map[string][]string
			if err := json.Unmarshal(aux.RawNotif, &newMap); err == nil {
				m.Notifications = NotificationMap(newMap)
			}
		}
	}

	// Process Warnings
	if len(aux.RawWarn) > 0 {
		var oldArray []string
		if err := json.Unmarshal(aux.RawWarn, &oldArray); err == nil {
			m.Warnings = unflattenNotifications(oldArray)
		} else {
			var newMap map[string][]string
			if err := json.Unmarshal(aux.RawWarn, &newMap); err == nil {
				m.Warnings = NotificationMap(newMap)
			}
		}
	}

	return nil
}

// ToggleNotification toggles a flat notification ID (e.g. "aqi_l1") in the nested map structure.
func (m *Monitor) ToggleNotification(id string) {
	parts := strings.SplitN(id, "_", 2)
	if len(parts) != 2 {
		return
	}
	cat, item := parts[0], parts[1]
	normItem := normalizeNotificationItem(item)
	if m.Notifications == nil {
		m.Notifications = make(NotificationMap)
	}
	found := -1
	for i, v := range m.Notifications[cat] {
		if normalizeNotificationItem(v) == normItem {
			found = i
			break
		}
	}
	if found >= 0 {
		m.Notifications[cat] = append(m.Notifications[cat][:found], m.Notifications[cat][found+1:]...)
	} else {
		m.Notifications[cat] = append(m.Notifications[cat], normItem)
	}
}

// ToggleWarning toggles a flat warning ID in the nested map structure.
func (m *Monitor) ToggleWarning(id string) {
	parts := strings.SplitN(id, "_", 2)
	if len(parts) != 2 {
		return
	}
	cat, item := parts[0], parts[1]
	normItem := normalizeNotificationItem(item)
	if m.Warnings == nil {
		m.Warnings = make(NotificationMap)
	}
	found := -1
	for i, v := range m.Warnings[cat] {
		if normalizeNotificationItem(v) == normItem {
			found = i
			break
		}
	}
	if found >= 0 {
		m.Warnings[cat] = append(m.Warnings[cat][:found], m.Warnings[cat][found+1:]...)
	} else {
		m.Warnings[cat] = append(m.Warnings[cat], normItem)
	}
}

// TgBot holds Telegram bot configuration.
type TgBot struct {
	// Enabled controls whether the bot is started at all.
	Enabled bool `yaml:"enabled"`
	// Token is the BotFather token for the Telegram bot.
	Token string `yaml:"token"`
	File  struct {
		Token string `yaml:"token"`
		Json  string `yaml:"json"`
	} `yaml:"file"`
	// Debug enables verbose Telegram API logging.
	Debug bool `yaml:"debug"`
	Chart struct {
		Width    int     `yaml:"width"`
		Height   int     `yaml:"height"`
		FontSize float64 `yaml:"font_size"`
	} `yaml:"chart"`
	Default struct {
		Unit struct {
			Temp  string `yaml:"temp"`
			Press string `yaml:"press"`
		} `yaml:"unit"`
	} `yaml:"default"`
	StartupRetries int `yaml:"startup_retries"`
	StartupDelay   int `yaml:"startup_delay"`
}

// NewTgBotConfig returns a TgBot with sensible defaults.
func NewTgBotConfig() *TgBot {
	app := getAppName()
	return &TgBot{
		Enabled: true,
		Token:   "",
		File: struct {
			Token string `yaml:"token"`
			Json  string `yaml:"json"`
		}{
			Token: app + ".tgbot.token",
			Json:  app + ".tgbot.json",
		},
		Debug: false,
		Chart: struct {
			Width    int     `yaml:"width"`
			Height   int     `yaml:"height"`
			FontSize float64 `yaml:"font_size"`
		}{
			Width:    1024,
			Height:   768,
			FontSize: 11.5,
		},
		Default: struct {
			Unit struct {
				Temp  string `yaml:"temp"`
				Press string `yaml:"press"`
			} `yaml:"unit"`
		}{
			Unit: struct {
				Temp  string `yaml:"temp"`
				Press string `yaml:"press"`
			}{
				Temp:  "c",
				Press: "mmhg",
			},
		},
		StartupRetries: 30,
		StartupDelay:   10,
	}
}

// DashboardEndpoint represents a custom dashboard HTTP endpoint definition.
type DashboardEndpoint struct {
	Path string `yaml:"path"`
	File string `yaml:"file"`
}

// DashboardsConfig contains general configuration for serving custom dashboards.
type DashboardsConfig struct {
	Enabled   bool                `yaml:"enabled"`
	Endpoints []DashboardEndpoint `yaml:"endpoints"`
}

type Config struct {
	System     System           `yaml:"system"`
	Server     Server           `yaml:"server"`
	Database   Database         `yaml:"database"`
	Monitor    Monitor          `yaml:"monitor"`
	Log        Log              `yaml:"log"`
	TgBot      TgBot            `yaml:"tgbot"`
	Dashboards DashboardsConfig `yaml:"dashboards"`
}

// NewConfig returns a Config initialized with defaults for all sub-sections.
func NewConfig() *Config {
	return &Config{
		System:   *NewSystemConfig(),
		Server:   *NewServerConfig(),
		Database: *NewDatabaseConfig(),
		Monitor:  *NewMonitorConfig(),
		Log:      *NewLogConfig(),
		TgBot:    *NewTgBotConfig(),
		Dashboards: DashboardsConfig{
			Enabled: false,
		},
	}
}

// LoadFromFile reads YAML configuration from the given file and unmarshals
// it into the receiver. If fileName is empty, "{app}.yaml" is used (where
// {app} is the executable name).
//
// It returns a regular error on failure so callers can handle it in a
// conventional way (wrapping is used to preserve the underlying cause).
func (cfg *Config) LoadFromFile(fileName string) error {
	exeName := getAppName()
	exeDir := "."
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
		if strings.Contains(exeDir, "go-build") || strings.Contains(exeDir, "Temp") {
			exeDir = "."
		}
	}

	resolveAppPath := func(raw string) string {
		if raw == "" {
			return raw
		}
		resolved := strings.ReplaceAll(raw, "{app}", exeName)
		// If only a filename (no directory component) and we have exeDir,
		// place next to the executable.
		if filepath.Base(resolved) == resolved && exeDir != "." {
			return filepath.Join(exeDir, resolved)
		}
		return resolved
	}

	// 2. Determine and resolve the config file path
	if strings.TrimSpace(fileName) == "" {
		fileName = "{app}.yaml"
	}
	fileName = resolveAppPath(fileName)

	// 3. Read and unmarshal
	yamlFile, err := os.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("reading config file %q: %w", fileName, err)
	}
	// Unmarshal directly into the receiver pointed by cfg (not &cfg).
	if err := yaml.Unmarshal(yamlFile, cfg); err != nil {
		return fmt.Errorf("unmarshalling config file %q: %w", fileName, err)
	}

	// 4. Post-processing for other fields that support {app}
	if cfg.Database.File.Json != "" {
		cfg.Database.File.Json = resolveAppPath(cfg.Database.File.Json)
	}
	if cfg.Database.File.Pgsql != "" {
		cfg.Database.File.Pgsql = resolveAppPath(cfg.Database.File.Pgsql)
		// Load from pgsql_file if it exists
		if _, err := os.Stat(cfg.Database.File.Pgsql); err == nil {
			pgData, err := os.ReadFile(cfg.Database.File.Pgsql)
			if err == nil {
				if err := yaml.Unmarshal(pgData, &cfg.Database); err != nil {
					return fmt.Errorf("unmarshalling pgsql file %q: %w", cfg.Database.File.Pgsql, err)
				}
			}
		}
	}
	if cfg.TgBot.File.Token != "" {
		cfg.TgBot.File.Token = resolveAppPath(cfg.TgBot.File.Token)
	}
	if cfg.TgBot.File.Json != "" {
		cfg.TgBot.File.Json = resolveAppPath(cfg.TgBot.File.Json)
	}
	if cfg.Log.LogFile != "" {
		cfg.Log.LogFile = resolveAppPath(cfg.Log.LogFile)
	}

	// 5. Load token from file if not provided in YAML
	if cfg.TgBot.Token == "" && cfg.TgBot.File.Token != "" {
		tokenBytes, err := os.ReadFile(cfg.TgBot.File.Token)
		if err == nil {
			content := string(tokenBytes)
			// Handle UTF-16 BOM if present (common on Windows)
			if len(tokenBytes) >= 2 {
				if tokenBytes[0] == 0xff && tokenBytes[1] == 0xfe {
					// UTF-16 LE: remove BOM and null bytes
					content = strings.ReplaceAll(string(tokenBytes[2:]), "\x00", "")
				} else if tokenBytes[0] == 0xfe && tokenBytes[1] == 0xff {
					// UTF-16 BE: remove BOM and null bytes
					content = strings.ReplaceAll(string(tokenBytes[2:]), "\x00", "")
				}
			}
			cfg.TgBot.Token = strings.TrimSpace(content)
		}
	}

	return nil
}

// getAppName extracts the application name from the executable filename without extension.
func getAppName() string {
	exe, err := os.Executable()
	if err != nil {
		return "aqinotifier"
	}
	exeDir := filepath.Dir(exe)
	if strings.Contains(exeDir, "go-build") || strings.Contains(exeDir, "Temp") {
		return "aqinotifier"
	}
	name := filepath.Base(exe)
	if ext := filepath.Ext(name); ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	return name
}
