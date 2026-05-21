// Package storage encapsulates all persistent data storage logic, abstracting
// away the underlying persistence mechanism (JSON files, SQL database, or Dual Mode).
package storage

import (
	"database/sql"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/rs/zerolog/log"
)

type Storage struct {
	mu          sync.RWMutex
	cfg         *config.Config
	db          *sql.DB
	dbConnected bool

	monitorFileMu sync.RWMutex
	tgbotFileMu   sync.RWMutex

	// Channels for background manager
	configUpdate chan *config.Config
	closeChan    chan struct{}
}

// NewStorage initializes a new Storage engine based on the provided configuration.
// It performs the initial connection attempt and starts the background reconnect loop.
func NewStorage(cfg *config.Config) (*Storage, error) {
	hasJSON := cfg.Database.HasUse("json")
	dbProviderName := cfg.Database.DBProvider()

	var initialDB *sql.DB
	if dbProviderName != "" {
		log.Info().Str("provider", dbProviderName).Msg("storage: initial connection attempt...")
		var err error
		for i := 0; i < 3; i++ {
			initialDB, err = config.NewDB(cfg.Database)
			if err == nil {
				initialDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
				initialDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
				initialDB.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetime) * time.Second)
				break
			}
			log.Warn().Err(err).Msgf("storage: connection attempt %d/3 failed", i+1)
			if i < 2 {
				time.Sleep(2 * time.Second)
			}
		}

		if err != nil {
			if !hasJSON {
				return nil, err
			}
			if isJSONFileEmpty(cfg.TgBot.JsonFile) {
				return nil, err
			}
			log.Warn().Err(err).Msg("storage: database is unavailable at startup, falling back to JSON mode")
		}
	}

	s := &Storage{
		cfg:          cfg,
		db:           initialDB,
		dbConnected:  initialDB != nil,
		configUpdate: make(chan *config.Config, 1),
		closeChan:    make(chan struct{}),
	}

	if initialDB != nil {
		s.initMonitorTableLocked()
		s.initTgBotTableLocked()
		s.syncMonitorJSON()
		s.syncTgBotJSON()
	}

	go s.runConnectionManager()

	return s, nil
}

func (s *Storage) UpdateConfig(newCfg *config.Config) {
	select {
	case s.configUpdate <- newCfg:
	default:
	}
}

func (s *Storage) Close() {
	close(s.closeChan)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		_ = s.db.Close()
		s.db = nil
		s.dbConnected = false
	}
}

func (s *Storage) runConnectionManager() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		s.mu.RLock()
		activeCfg := s.cfg
		currentDB := s.db
		s.mu.RUnlock()

		provider := activeCfg.Database.DBProvider()
		dsn := activeCfg.Database.String()

		if provider == "" {
			if currentDB != nil {
				log.Info().Msg("storage: SQL mode disabled, closing connection pool")
				s.mu.Lock()
				_ = s.db.Close()
				s.db = nil
				s.dbConnected = false
				s.mu.Unlock()
			}
		} else {
			if currentDB == nil {
				log.Info().Str("provider", provider).Msg("storage: connecting to database...")
				db, err := config.NewDB(activeCfg.Database)
				if err == nil {
					db.SetMaxOpenConns(activeCfg.Database.MaxOpenConns)
					db.SetMaxIdleConns(activeCfg.Database.MaxIdleConns)
					db.SetConnMaxLifetime(time.Duration(activeCfg.Database.ConnMaxLifetime) * time.Second)

					s.mu.Lock()
					s.db = db
					s.dbConnected = true
					s.initMonitorTableLocked()
					s.initTgBotTableLocked()
					s.mu.Unlock()

					s.syncMonitorJSON()
					s.syncTgBotJSON()
				} else {
					log.Warn().Err(err).Msg("storage: database connection failed, will retry in 10s")
				}
			} else {
				if err := currentDB.Ping(); err != nil {
					log.Warn().Err(err).Msg("storage: database connection lost, closing pool and will retry in 10s")
					s.mu.Lock()
					_ = s.db.Close()
					s.db = nil
					s.dbConnected = false
					s.mu.Unlock()
				}
			}
		}

		select {
		case newCfg := <-s.configUpdate:
			s.mu.Lock()
			s.cfg = newCfg
			s.mu.Unlock()
			// If DSN or Provider changed, we force reconnect next loop
			if newCfg.Database.DBProvider() != provider || newCfg.Database.String() != dsn {
				s.mu.Lock()
				if s.db != nil {
					log.Info().Msg("storage: closing old database connection pool due to config change")
					_ = s.db.Close()
					s.db = nil
					s.dbConnected = false
				}
				s.mu.Unlock()
			}
		case <-ticker.C:
		case <-s.closeChan:
			return
		}
	}
}

func isJSONFileEmpty(path string) bool {
	if path == "" {
		return true
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) || info.Size() == 0 {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "[]" || trimmed == "{}" {
		return true
	}
	return false
}
