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
	"github.com/ABespalov/aqinotifier/tgbot"
	"github.com/rs/zerolog/log"
)

// main is the entry point of the application. It loads the configuration,
// initializes the logger, monitor service, Telegram bot, and starts the HTTP server.
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

	// Initialize zerolog
	_, cleanup, err := config.NewLogger(cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	ms := monitor.NewMonitorService(cfg)

	// Channels to signal restarts
	restartServer := make(chan struct{}, 1)
	restartBot := make(chan struct{}, 1)

	// Config reload goroutine
	if cfg.System.ConfigReloadTime > 0 {
		go func() {
			info, err := os.Stat(configName)
			if err != nil {
				log.Error().Err(err).Msg("config reload: failed to stat config file")
				return
			}
			lastMod := info.ModTime()

			ticker := time.NewTicker(time.Duration(cfg.System.ConfigReloadTime) * time.Second)
			defer ticker.Stop()

			for range ticker.C {
				info, err := os.Stat(configName)
				if err != nil {
					continue
				}
				if info.ModTime().After(lastMod) {
					log.Info().Msg("config reload: file changed, reloading...")
					newCfg := config.NewConfig()
					if err := newCfg.LoadFromFile(configName); err != nil {
						log.Error().Err(err).Msg("config reload: failed to load new config")
					} else {
						// Capture old settings for comparison
						oldNode := cfg.Server.Node()
						oldProto := cfg.Server.Protocol
						oldUrl := cfg.Server.Url
						oldCert := cfg.Server.CertFile
						oldKey := cfg.Server.KeyFile
						oldTgEnabled := cfg.TgBot.Enabled
						oldTgToken := cfg.TgBot.Token

						// Update existing config values
						*cfg = *newCfg
						lastMod = info.ModTime()
						log.Info().Msgf("config reload: success (new chart width: %d, height: %d, fontSize: %.1f)", cfg.TgBot.ChartWidth, cfg.TgBot.ChartHeight, cfg.TgBot.ChartFontSize)

						// Check if server needs restart
						if cfg.Server.Node() != oldNode ||
							cfg.Server.Protocol != oldProto ||
							cfg.Server.Url != oldUrl ||
							cfg.Server.CertFile != oldCert ||
							cfg.Server.KeyFile != oldKey {
							log.Info().Msg("config reload: server settings changed, signaling restart")
							select {
							case restartServer <- struct{}{}:
							default:
							}
						}

						// Check if bot needs restart
						if cfg.TgBot.Enabled != oldTgEnabled || cfg.TgBot.Token != oldTgToken {
							log.Info().Msg("config reload: bot settings changed, signaling restart")
							select {
							case restartBot <- struct{}{}:
							default:
							}
						}
					}
				}
			}
		}()
	}

	var bot *tgbot.Bot
	var srv *http.Server

	// Bot management loop
	go func() {
		for {
			// Start Telegram bot if enabled
			if cfg.TgBot.Enabled {
				if cfg.TgBot.Token == "" {
					log.Error().Msg("tgbot.enabled is true but tgbot.token is not set")
				} else {
					var err error
					bot, err = tgbot.NewBot(&cfg.TgBot, &cfg.Monitor, ms)
					if err != nil {
						log.Error().Err(err).Msg("failed to start Telegram bot")
					} else {
						ms.SetNotifier(bot)
						go bot.Run()
						log.Info().Str("json_file", cfg.TgBot.JsonFile).Msg("tgbot: started")
					}
				}
			} else {
				ms.SetNotifier(nil)
				log.Debug().Msg("tgbot: disabled")
			}

			// Wait for restart signal
			<-restartBot
			if bot != nil {
				bot.Stop()
				bot = nil
			}
			log.Info().Msg("tgbot: restarting...")
		}
	}()

	// Server management loop
	for {
		log.Info().Msgf("starting server at %s", cfg.Server.Node())

		// Debug logs for config
		log.Debug().Msgf("system: values_in_ram=%d, config_reload_time=%d",
			cfg.System.ValuesInRam, cfg.System.ConfigReloadTime)
		log.Debug().Msgf("protocol: %s", cfg.Server.Protocol)
		log.Debug().Msgf("server timeouts: server=%ds, read=%ds, write=%ds, idle=%ds",
			cfg.Server.Timeout.Server, cfg.Server.Timeout.Read, cfg.Server.Timeout.Write, cfg.Server.Timeout.Idle)

		if cfg.Database.Type == "json" {
			log.Debug().Msgf("db: type=%s, file=%s, max_values=%d",
				cfg.Database.Type, cfg.Database.JsonFile, cfg.Database.MaxValues)
		} else {
			log.Debug().Msgf("db: type=%s", cfg.Database.Type)
		}

		log.Debug().Msgf("monitor: pm10_val=%.1f, pm25_val=%.1f, diff_time=%ds, pm10_diff=%.1f%%, pm25_diff=%.1f%%",
			cfg.Monitor.PM10Value, cfg.Monitor.PM25Value, cfg.Monitor.DiffTime, cfg.Monitor.PM10Diff, cfg.Monitor.PM25Diff)

		log.Debug().Msgf("listening at %s", cfg.Server.Url)

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
				if cfg.Server.CertFile == "" || cfg.Server.KeyFile == "" {
					log.Error().Msg("HTTPS requested but cert_file or key_file is empty")
					return
				}
				err = srv.ListenAndServeTLS(cfg.Server.CertFile, cfg.Server.KeyFile)
			} else {
				err = srv.ListenAndServe()
			}
			if err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("server failed")
			}
		}()

		// Wait for restart signal
		<-restartServer
		log.Info().Msg("server: stopping for restart...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
		log.Info().Msg("server: stopped")
	}
}

// apiHandler processes incoming POST requests from sensors, parses the JSON payload,
// and passes the data to the monitor service.
func apiHandler(w http.ResponseWriter, r *http.Request, ms *monitor.MonitorService) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		log.Warn().Str("method", r.Method).Msg("wrong http method")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		log.Error().Err(err).Msg("error reading request body")
		return
	}

	data, err := sensor.Parse(r.RemoteAddr, body)
	if err != nil {
		http.Error(w, "Error unmarshaling JSON", http.StatusBadRequest)
		log.Error().Err(err).Str("ip", r.RemoteAddr).Msg("error unmarshaling json")
		return
	}

	// Log data receipt
	var pm10, pm25 string
	for _, v := range data.Values {
		switch v.Type {
		case "SDS_P1":
			pm10 = v.Value
		case "SDS_P2":
			pm25 = v.Value
		}
	}

	log.Info().
		Str("ip", r.RemoteAddr).
		Str("id", data.ParentID).
		Int("size", len(body)).
		Str("pm10", pm10).
		Str("pm25", pm25).
		Msg("received data")

	// Process data through monitor service
	ms.Process(data)

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Data received successfully!")
}
