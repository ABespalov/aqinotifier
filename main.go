package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/ABespalov/aqinotifier/tgbot"
	"github.com/rs/zerolog/log"
)

const BotVersion = "0.10.44"

func main() {
	execPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get executable path: %v\n", err)
		os.Exit(1)
	}
	configName := strings.TrimSuffix(filepath.Base(execPath), filepath.Ext(execPath)) + ".yaml"

	cfg := config.NewConfig()
	if err := cfg.LoadFromFile(configName); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config %s: %v\n", configName, err)
		os.Exit(1)
	}

	_, cleanup, err := config.NewLogger(cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	activeCleanup := cleanup
	defer func() {
		if activeCleanup != nil {
			activeCleanup()
		}
	}()

	log.Info().Str("version", BotVersion).Msg("🌬️ AQI Notifier Bot starting...")

	if cfg.Database.JsonFile == "" {
		fmt.Fprintf(os.Stderr, "database.json_file is required for the application to function\n")
		os.Exit(1)
	}

	var bot *tgbot.Bot
	ms := monitor.NewMonitorService(cfg)
	
	// Database initialization with reconnection logic
	var db *sql.DB
	if cfg.Database.Type == "postgres" {
		for i := 0; i < 20; i++ {
			db, err = config.NewDB(cfg.Database)
			if err == nil {
				// Configure connection pool
				db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
				db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
				db.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetime) * time.Second)
				break
			}
			log.Warn().Err(err).Msgf("db: connection failed, retrying in 10s... (%d/20)", i+1)
			time.Sleep(10 * time.Second)
		}
		if err != nil {
			log.Error().Err(err).Msg("db: failed to connect to postgres after retries, falling back to JSON")
		} else {
			ms.SetDB(db)
		}
	}

	// Database reconnection and background sync worker
	if cfg.Database.Type == "postgres" && db != nil {
		go func() {
			ticker := time.NewTicker(1 * time.Minute)
			for range ticker.C {
				if err := db.Ping(); err != nil {
					log.Warn().Err(err).Msg("db: connection lost, background ping failed")
				}
			}
		}()
	}

	restartServer := make(chan struct{}, 1)
	restartBot := make(chan struct{}, 1)

	if cfg.System.ConfigReloadTime > 0 {
		go func() {
			getWatchList := func(c *config.Config) []string {
				return []string{
					configName,
					c.Server.CertFile,
					c.Server.KeyFile,
					c.TgBot.TokenFile,
					c.Database.PgsqlFile,
				}
			}

			lastMod := make(map[string]time.Time)
			updateModTimes := func(c *config.Config) {
				for _, f := range getWatchList(c) {
					if f != "" {
						if info, err := os.Stat(f); err == nil {
							lastMod[f] = info.ModTime()
						}
					}
				}
			}

			updateModTimes(cfg)
			ticker := time.NewTicker(time.Duration(cfg.System.ConfigReloadTime) * time.Second)
			for range ticker.C {
				changed := false
				for _, f := range getWatchList(cfg) {
					if f == "" {
						continue
					}
					if info, err := os.Stat(f); err == nil {
						if info.ModTime().After(lastMod[f]) {
							changed = true
							break
						}
					}
				}

				if changed {
					newCfg := config.NewConfig()
					if err := newCfg.LoadFromFile(configName); err != nil {
						log.Error().Err(err).Msg("config reload: failed to load new config")
						// Update mod times even on failure to avoid looping on a broken file
						updateModTimes(cfg)
						continue
					}

					oldNode := cfg.Server.Node()
					oldProto := cfg.Server.Protocol
					oldUrl := cfg.Server.Url
					oldCert := cfg.Server.CertFile
					oldKey := cfg.Server.KeyFile
					oldTgEnabled := cfg.TgBot.Enabled
					oldTgToken := cfg.TgBot.Token
					oldLogLevel := cfg.Log.Level
					oldLogFile := cfg.Log.LogFile

					*cfg = *newCfg
					updateModTimes(cfg)
					log.Info().Msg("config reload: success")

					if cfg.Log.Level != oldLogLevel || cfg.Log.LogFile != oldLogFile {
						log.Info().Msg("config reload: updating logger...")
						if activeCleanup != nil {
							activeCleanup()
						}
						_, activeCleanup, _ = config.NewLogger(cfg.Log)
					}

					if cfg.Server.Node() != oldNode || cfg.Server.Protocol != oldProto || cfg.Server.Url != oldUrl || cfg.Server.CertFile != oldCert || cfg.Server.KeyFile != oldKey {
						select {
						case restartServer <- struct{}{}:
						default:
						}
					}
					if cfg.TgBot.Enabled != oldTgEnabled || cfg.TgBot.Token != oldTgToken {
						select {
						case restartBot <- struct{}{}:
						default:
						}
					}
				}
			}
		}()
	}

	var srv *http.Server

	go func() {
		for {
			if cfg.TgBot.Enabled {
				if cfg.TgBot.Token == "" {
					log.Error().Msg("tgbot.enabled is true but tgbot.token is not set")
				} else {
					var err error
					// Retry loop for bot startup (e.g. no internet/DNS at boot)
					for i := 0; i < 30; i++ {
						bot, err = tgbot.NewBot(cfg, &cfg.Monitor, ms, BotVersion)
						if err == nil {
							break
						}
						log.Warn().Err(err).Msgf("failed to start Telegram bot, retrying in 10s... (%d/30)", i+1)
						time.Sleep(10 * time.Second)
					}

					if err != nil {
						log.Error().Err(err).Msg("failed to start Telegram bot after retries")
					} else {
						if db != nil {
							bot.SetDB(db)
						}
						ms.SetNotifier(bot)
						go bot.Run()
						log.Info().Str("json_file", cfg.TgBot.JsonFile).Msg("tgbot: started")
					}
				}
			} else {
				ms.SetNotifier(nil)
				log.Debug().Msg("tgbot: disabled")
			}
			<-restartBot
			if bot != nil {
				bot.Stop()
				bot = nil
			}
			log.Info().Msg("tgbot: restarting...")
		}
	}()

	for {
		log.Info().Msgf("starting server at %s", cfg.Server.Node())
		mux := http.NewServeMux()
		mux.HandleFunc(cfg.Server.Url, func(w http.ResponseWriter, r *http.Request) {
			apiHandler(w, r, ms)
		})
		srv = &http.Server{
			Addr:    cfg.Server.Node(),
			Handler: mux,
		}
		go func() {
			var err error
			if cfg.Server.Protocol == "https" {
				err = srv.ListenAndServeTLS(cfg.Server.CertFile, cfg.Server.KeyFile)
			} else {
				err = srv.ListenAndServe()
			}
			if err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("server failed")
			}
		}()
		<-restartServer
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}
}

func apiHandler(w http.ResponseWriter, r *http.Request, ms *monitor.MonitorService) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading body", http.StatusInternalServerError)
		return
	}
	data, err := sensor.Parse(r.RemoteAddr, body)
	if err != nil {
		http.Error(w, "Error parsing JSON", http.StatusBadRequest)
		return
	}
	ms.Process(data)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Data received")
}
