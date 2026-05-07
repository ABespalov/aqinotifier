package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
)

const AppVersion = "0.9.1a"

// Server holds HTTP server binding settings and the URL path used by the
// application to receive POST requests from sensors.
type Server struct {
	Host     string        `yaml:"host"`
	Port     int           `yaml:"port"`
	Url      string        `yaml:"url"`
	Protocol string        `yaml:"protocol"`
	CertFile string        `yaml:"cert_file"`
	KeyFile  string        `yaml:"key_file"`
	Timeout  ServerTimeout `yaml:"timeout"`
}

// ServerTimeout groups timeout settings (in seconds) used by the HTTP server.
// Typical usage is to apply these values to net/http.Server.ReadTimeout,
// WriteTimeout and IdleTimeout.
type ServerTimeout struct {
	Server int `yaml:"server"`
	Read   int `yaml:"read"`
	Write  int `yaml:"write"`
	Idle   int `yaml:"idle"`
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
		CertFile: "localhost.pem",
		KeyFile:  "localhost-key.pem",
		Timeout: ServerTimeout{
			Server: 30,
			Read:   15,
			Write:  15,
			Idle:   5,
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
	// Supported types: "postgres", "json".
	// Logic: data is always written to JSON (if json_file is set).
	// If type is "json", data is ONLY written to JSON.
	// If type is "postgres", data is written to Postgres AND JSON (for stability).
	Type string `yaml:"type"`
	// JSON file settings (always used for persistence and stability).
	// If json_file is not set, the application cannot function.
	JsonFile        string `yaml:"json_file"`
	PgsqlFile       string `yaml:"pgsql_file"`
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
}

// NewDatabaseConfig returns a Database populated with conservative defaults
// suitable for local development. Adjust SslMode and pool sizes for
// production environments.
func NewDatabaseConfig() *Database {
	app := getAppName()
	return &Database{
		Type:            "postgres",
		JsonFile:        app + ".data.json",
		PgsqlFile:       app + ".pgsql",
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
	}
}

// String returns a DSN-like representation of the database settings.
// Note: it embeds the password in the output — avoid logging this in
// production. Use this primarily for diagnostics in trusted contexts.
func (d Database) String() string {
	return fmt.Sprintf("%s://%s:%s@%s:%d/%s", d.Type, d.User, d.Password, d.Host, d.Port, d.Db)
}

type System struct {
	ValuesInRam      int `yaml:"values_in_ram"`
	ConfigReloadTime int `yaml:"config_reload_time"`
}

func NewSystemConfig() *System {
	return &System{
		ValuesInRam:      10,
		ConfigReloadTime: 5,
	}
}

type Monitor struct {
	PM10Green    float64  `yaml:"pm10_green" json:"pm10_green"`
	PM25Green    float64  `yaml:"pm25_green" json:"pm25_green"`
	PM10Yellow   float64  `yaml:"pm10_yellow" json:"pm10_yellow"`
	PM25Yellow   float64  `yaml:"pm25_yellow" json:"pm25_yellow"`
	PM10Diff     float64  `yaml:"pm10_diff" json:"pm10_diff"`
	PM25Diff     float64  `yaml:"pm25_diff" json:"pm25_diff"`
	Notifications []string `yaml:"notifications" json:"notifications"`
	Warnings      []string          `yaml:"warnings" json:"warnings"`
	AQIStandard   string            `yaml:"aqi_standard" json:"aqi_standard"`
	DeviceNames   map[string]string `yaml:"device_names" json:"device_names"`

	// Migration fields (deprecated)
	PM10Value       float64  `yaml:"-" json:"pm10_value,omitempty"`
	PM25Value       float64  `yaml:"-" json:"pm25_value,omitempty"`
	OldLoudWarnings []string `yaml:"-" json:"loud_warnings,omitempty"`
}

// NewMonitorConfig returns a Monitor pre-populated with default values
func NewMonitorConfig() *Monitor {
	return &Monitor{
		PM10Green:     54.0,
		PM25Green:     9.0,
		PM10Yellow:    154.0,
		PM25Yellow:    35.0,
		PM10Diff:      66.0,
		PM25Diff:      50.0,
		Notifications: []string{"aqi_z1", "aqi_z2", "aqi_z3", "aqi_z4", "aqi_z5", "aqi_z6", "aqi_z7", "vals-ru", "vals-gd"},
		Warnings:      []string{"aqi_z1", "aqi_z2", "aqi_z3", "aqi_z4", "aqi_z5", "aqi_z6", "aqi_z7", "val25-yu", "val25-ru", "val25-yd", "val25-gd", "val10-yu", "val10-ru", "val10-yd", "val10-gd", "vals-yu", "vals-ru", "vals-yd", "vals-gd"},
		AQIStandard:   "EU",
		DeviceNames:   make(map[string]string),
	}
}

// TgBot holds Telegram bot configuration.
type TgBot struct {
	// Enabled controls whether the bot is started at all.
	Enabled bool `yaml:"enabled"`
	// Token is the BotFather token for the Telegram bot.
	Token string `yaml:"token"`
	// TokenFile is the path to the file containing the bot token.
	TokenFile string `yaml:"token_file"`
	// JsonFile is the path to the JSON file used to persist subscriptions.
	// Supports the {app} placeholder (replaced with the executable name).
	JsonFile string `yaml:"json_file"`
	// Debug enables verbose Telegram API logging.
	Debug bool `yaml:"debug"`
	// ChartWidth specifies the width of generated charts.
	ChartWidth int `yaml:"chart_width"`
	// ChartHeight specifies the height of generated charts.
	ChartHeight int `yaml:"chart_height"`
	// ChartFontSize specifies the font size used in generated charts.
	ChartFontSize float64 `yaml:"chart_font_size"`
	// Default units
	DefaultUnitTemp  string `yaml:"default_unit_temp"`
	DefaultUnitPress string `yaml:"default_unit_press"`
}

// NewTgBotConfig returns a TgBot with sensible defaults.
func NewTgBotConfig() *TgBot {
	app := getAppName()
	return &TgBot{
		Enabled:       true,
		Token:         "",
		TokenFile:     app + ".tgbot.token",
		JsonFile:      app + ".tgbot.json",
		Debug:         false,
		ChartWidth:    1024,
		ChartHeight:   768,
		ChartFontSize: 12.0,
		DefaultUnitTemp:  "c",
		DefaultUnitPress: "mmhg",
	}
}

type Config struct {
	System   System   `yaml:"system"`
	Server   Server   `yaml:"server"`
	Database Database `yaml:"database"`
	Monitor  Monitor  `yaml:"monitor"`
	Log      Log      `yaml:"log"`
	TgBot    TgBot    `yaml:"tgbot"`
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
	if cfg.Database.JsonFile != "" {
		cfg.Database.JsonFile = resolveAppPath(cfg.Database.JsonFile)
	}
	if cfg.Database.PgsqlFile != "" {
		cfg.Database.PgsqlFile = resolveAppPath(cfg.Database.PgsqlFile)
		// Load from pgsql_file if it exists
		if _, err := os.Stat(cfg.Database.PgsqlFile); err == nil {
			pgData, err := os.ReadFile(cfg.Database.PgsqlFile)
			if err == nil {
				if err := yaml.Unmarshal(pgData, &cfg.Database); err != nil {
					return fmt.Errorf("unmarshalling pgsql file %q: %w", cfg.Database.PgsqlFile, err)
				}
			}
		}
	}
	if cfg.TgBot.TokenFile != "" {
		cfg.TgBot.TokenFile = resolveAppPath(cfg.TgBot.TokenFile)
	}
	if cfg.TgBot.JsonFile != "" {
		cfg.TgBot.JsonFile = resolveAppPath(cfg.TgBot.JsonFile)
	}
	if cfg.Log.LogFile != "" {
		cfg.Log.LogFile = resolveAppPath(cfg.Log.LogFile)
	}

	// 5. Load token from file if not provided in YAML
	if cfg.TgBot.Token == "" && cfg.TgBot.TokenFile != "" {
		tokenBytes, err := os.ReadFile(cfg.TgBot.TokenFile)
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
