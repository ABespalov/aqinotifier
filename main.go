// Package main is the entry point of the AQI Notifier Bot application.
// It initializes the configuration, logger, monitor service, Telegram bot,
// and starts the HTTPS server to receive sensor data. It also manages the
// database connection manager (including auto-reconnect and health checks)
// and handles configuration hot-reloading at runtime.
package main

import (
	"context"
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
	"github.com/ABespalov/aqinotifier/storage"
	"github.com/ABespalov/aqinotifier/tgbot"
	"github.com/rs/zerolog/log"
)

const BotVersion = "0.17.0a"

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

	// 1. Check if database.use is empty (fatality check)
	if len(cfg.Database.Use) == 0 {
		log.Fatal().Msg("FATAL: database.use is empty. At least one persistence mode ('postgres' or 'json') must be specified")
	}

	strg, err := storage.NewStorage(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("FATAL: failed to initialize storage")
	}
	defer strg.Close()

	var bot *tgbot.Bot
	ms := monitor.NewMonitorService(cfg, strg)

	restartServer := make(chan struct{}, 1)
	restartBot := make(chan struct{}, 1)

	if cfg.System.ConfigReloadTime > 0 {
		go func() {
			getWatchList := func(c *config.Config) []string {
				return []string{
					configName,
					c.Server.File.Cert,
					c.Server.File.Key,
					c.TgBot.File.Token,
					c.Database.File.Pgsql,
					filepath.Join("res", "ru.json"),
					filepath.Join("res", "en.json"),
					filepath.Join("res", "ico.json"),
					filepath.Join("res", "colors.json"),
					filepath.Join("res", "aqi.json"),
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
				trackedFiles := getWatchList(cfg)
				var changedFiles []string
				for _, f := range trackedFiles {
					if f == "" {
						continue
					}
					if info, err := os.Stat(f); err == nil {
						if info.ModTime().After(lastMod[f]) {
							changedFiles = append(changedFiles, f)
							changed = true
						}
					}
				}

				if changed {
					log.Info().Strs("files", changedFiles).Msg("config reload: detected file changes")
					newCfg := config.NewConfig()
					if err := newCfg.LoadFromFile(configName); err != nil {
						log.Error().Err(err).Msg("config reload: failed to load new config")
						// Update mod times even on failure to avoid looping on a broken file
						updateModTimes(cfg)
						continue
					}

					if len(newCfg.Database.Use) == 0 {
						log.Error().Msg("config reload: database.use cannot be empty")
						updateModTimes(cfg)
						continue
					}

					oldNode := cfg.Server.Node()
					oldProto := cfg.Server.Protocol
					oldUrl := cfg.Server.Url
					oldCert := cfg.Server.File.Cert
					oldKey := cfg.Server.File.Key
					oldTgEnabled := cfg.TgBot.Enabled
					oldTgToken := cfg.TgBot.Token
					oldLogLevel := cfg.Log.Level
					oldLogFile := cfg.Log.LogFile

					*cfg = *newCfg
					updateModTimes(cfg)
					tgbot.ReloadAll()
					ms.Reload()
					log.Info().Msg("config reload: success (including translations & evaluators)")

					strg.UpdateConfig(cfg)

					if cfg.Log.Level != oldLogLevel || cfg.Log.LogFile != oldLogFile {
						log.Info().Msg("config reload: updating logger...")
						if activeCleanup != nil {
							activeCleanup()
						}
						_, newCleanup, logErr := config.NewLogger(cfg.Log)
						if logErr != nil {
							log.Error().Err(logErr).Msg("config reload: failed to reinitialize logger")
						} else {
							activeCleanup = newCleanup
						}
					}

					if cfg.Server.Node() != oldNode || cfg.Server.Protocol != oldProto || cfg.Server.Url != oldUrl || cfg.Server.File.Cert != oldCert || cfg.Server.File.Key != oldKey {
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
					for i := 0; i < cfg.TgBot.StartupRetries; i++ {
						bot, err = tgbot.NewBot(cfg, &cfg.Monitor, ms, strg, BotVersion)
						if err == nil {
							break
						}
						log.Warn().Err(err).Msgf("failed to start Telegram bot, retrying in %ds... (%d/%d)", cfg.TgBot.StartupDelay, i+1, cfg.TgBot.StartupRetries)
						time.Sleep(time.Duration(cfg.TgBot.StartupDelay) * time.Second)
					}

					if err != nil {
						log.Error().Err(err).Msg("failed to start Telegram bot after retries")
					} else {

						ms.SetNotifier(bot)
						go bot.Run()
						log.Info().Str("json_file", cfg.TgBot.File.Json).Msg("tgbot: started")
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
				err = srv.ListenAndServeTLS(cfg.Server.File.Cert, cfg.Server.File.Key)
			} else {
				err = srv.ListenAndServe()
			}
			if err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("server failed")
			}
		}()
		<-restartServer
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Server.Timeout.Shutdown)*time.Second)
		if err := srv.Shutdown(ctx); err != nil {
			log.Warn().Err(err).Msg("server: shutdown did not complete cleanly")
		}
		cancel()
	}
}

func apiHandler(w http.ResponseWriter, r *http.Request, ms *monitor.MonitorService) {
	log.Debug().Str("method", r.Method).Str("remote", r.RemoteAddr).Str("url", r.URL.String()).Msg("server: received request")

	if r.Method != http.MethodPost {
		log.Warn().Str("method", r.Method).Str("remote", r.RemoteAddr).Msg("server: rejected non-POST request")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error().Err(err).Msg("server: error reading body")
		http.Error(w, "Error reading body", http.StatusInternalServerError)
		return
	}
	data, err := sensor.Parse(r.RemoteAddr, body)
	if err != nil {
		log.Error().Err(err).Str("body", string(body)).Msg("server: error parsing JSON")
		http.Error(w, "Error parsing JSON", http.StatusBadRequest)
		return
	}
	ms.Process(data)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Data received")
}
