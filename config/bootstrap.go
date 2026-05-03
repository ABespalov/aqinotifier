package config

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// NewLogger creates and returns a zerolog.Logger configured according to the
// Log section from the configuration. It also returns a cleanup function
// that should be called on program shutdown to allow log writers to flush
// (noop when logging to stdout).
//
// Behavior:
//   - if cfg.File is empty, logs are written to stdout.
//   - otherwise a lumberjack rotating file writer is created using cfg.Rotate
//     settings and used as zerolog output.
func NewLogger(cfg Log) (zerolog.Logger, func(), error) {
	var writers []io.Writer

	// 1. Console output (always human-friendly)
	writers = append(writers, zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339})

	// 2. File output (always JSON for structured logging)
	var fileWriter io.WriteCloser
	if cfg.File != "" {
		lj := &lumberjack.Logger{
			Filename:   cfg.File,
			MaxSize:    cfg.Rotate.MaxSizeMB,
			MaxBackups: cfg.Rotate.MaxBackups,
			MaxAge:     cfg.Rotate.MaxAgeDays,
			Compress:   cfg.Rotate.Compress,
		}
		writers = append(writers, lj)
		fileWriter = lj
	}

	multi := zerolog.MultiLevelWriter(writers...)
	logger := zerolog.New(multi).With().Timestamp().Logger()

	// set level (case-insensitive)
	lvl := strings.ToLower(cfg.Level)
	switch lvl {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		logger = logger.Level(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		logger = logger.Level(zerolog.InfoLevel)
	case "warn", "warning":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
		logger = logger.Level(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
		logger = logger.Level(zerolog.ErrorLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
		logger = logger.Level(zerolog.FatalLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		logger = logger.Level(zerolog.InfoLevel)
	}

	// set the global logger
	log.Logger = logger

	log.Info().Str("level", zerolog.GlobalLevel().String()).Msg("log: logger initialized")

	cleanup := func() {
		if fileWriter != nil {
			_ = fileWriter.Close()
		}
	}

	return logger, cleanup, nil
}

// NewDB creates a *sql.DB according to the Database config and applies pool
// tuning parameters. The function supports Postgres via the lib/pq driver.
func NewDB(cfg Database) (*sql.DB, error) {
	if cfg.Type != "postgres" {
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Type)
	}

	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=xxxx dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Db, cfg.SslMode,
	)
	log.Info().Str("dsn", dsn).Msg("db: connecting to postgres...")

	realDSN := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Db, cfg.SslMode,
	)

	db, err := sql.Open("postgres", realDSN)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// apply pool settings
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)
	}

	// verify connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	log.Info().Msg("db: postgres connected successfully")
	return db, nil
}
