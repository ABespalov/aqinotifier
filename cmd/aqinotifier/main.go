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

	"github.com/ABespalov/aqinotifier/internal/config"
	"github.com/ABespalov/aqinotifier/internal/dashboard"
	"github.com/ABespalov/aqinotifier/internal/monitor"
	"github.com/ABespalov/aqinotifier/internal/sensor"
	"github.com/ABespalov/aqinotifier/internal/storage"
	"github.com/ABespalov/aqinotifier/internal/tgbot"
	"github.com/ABespalov/csi18n"
	"github.com/rs/zerolog/log"
)

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

	translator := csi18n.New("en", "en")
	resDir := filepath.Join(".", "assets")
	_ = translator.LoadDir(resDir)

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
					filepath.Join("assets", "ru.json"),
					filepath.Join("assets", "en.json"),
					filepath.Join("assets", "ico.json"),
					filepath.Join("assets", "colors.json"),
					filepath.Join("assets", "aqi.json"),
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
					oldDashboardsEnabled := cfg.Dashboards.Enabled
					oldEndpoints := cfg.Dashboards.Endpoints

					*cfg = *newCfg
					updateModTimes(cfg)
					_ = translator.LoadDir(resDir)
					updateAQIStandards(translator, resDir)
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

					dashboardsChanged := cfg.Dashboards.Enabled != oldDashboardsEnabled || !endpointsEqual(cfg.Dashboards.Endpoints, oldEndpoints)
					if dashboardsChanged || cfg.Server.Node() != oldNode || cfg.Server.Protocol != oldProto || cfg.Server.Url != oldUrl || cfg.Server.File.Cert != oldCert || cfg.Server.File.Key != oldKey {
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
						bot, err = tgbot.NewBot(cfg, &cfg.Monitor, ms, strg, translator, BotVersion)
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
		if cfg.Dashboards.Enabled {
			dashboard.RegisterHandlers(mux, cfg, ms, translator)
		}
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

func updateAQIStandards(translator *csi18n.Translator, resDir string) {
	aqiPath := filepath.Join(resDir, "aqi.json")
	if data, err := os.ReadFile(aqiPath); err == nil {
		if err := sensor.LoadStandards(data); err != nil {
			log.Error().Err(err).Msg("failed to load AQI standards")
		} else {
			stds := sensor.GetStandards()
			for tag, std := range stds {
				tagUpper := strings.ToUpper(tag)
				flagKey := fmt.Sprintf("icoFlag%s", tagUpper)
				if icon := translator.T("en", flagKey); !strings.HasPrefix(icon, "!!") {
					std.Flag = icon
				}
				stdKey := "standard" + strings.Title(strings.ToLower(tag))
				if name := translator.T("en", stdKey); !strings.HasPrefix(name, "!!") {
					std.NameFull = name
				}
				shortKey := "txtStandard" + strings.Title(strings.ToLower(tag))
				if name := translator.T("en", shortKey); !strings.HasPrefix(name, "!!") {
					std.NameShort = name
				}

				for i := range std.Zones {
					level := std.Zones[i].Level
					nameKey := fmt.Sprintf("aqiNameL%d%s", level, strings.Title(strings.ToLower(tag)))
					if name := translator.T("en", nameKey); !strings.HasPrefix(name, "!!") {
						std.Zones[i].Name = name
					}
					iconKey := fmt.Sprintf("icoAqi%sLevel%d", tagUpper, level)
					if icon := translator.T("en", iconKey); !strings.HasPrefix(icon, "!!") {
						std.Zones[i].Icon = icon
					}
				}
			}
		}
	} else {
		log.Warn().Err(err).Msg("failed to read AQI standards file")
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

// endpointsEqual checks if two lists of DashboardEndpoints are identical.
func endpointsEqual(a, b []config.DashboardEndpoint) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].File != b[i].File {
			return false
		}
	}
	return true
}
