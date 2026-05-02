package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/ABespalov/aqinotifier/tgbot"
	"github.com/rs/zerolog/log"
)

func main() {
	cfg := config.NewConfig()
	if err := cfg.LoadFromFile(""); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize zerolog
	_, cleanup, err := config.NewLogger(cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	// Config reload goroutine
	if cfg.System.ConfigReloadTime > 0 {
		go func() {
			fileName := "aqinotifier.yaml" // Default
			info, err := os.Stat(fileName)
			if err != nil {
				log.Error().Err(err).Msg("config reload: failed to stat config file")
				return
			}
			lastMod := info.ModTime()

			ticker := time.NewTicker(time.Duration(cfg.System.ConfigReloadTime) * time.Second)
			defer ticker.Stop()

			for range ticker.C {
				info, err := os.Stat(fileName)
				if err != nil {
					continue
				}
				if info.ModTime().After(lastMod) {
					log.Info().Msg("config reload: file changed, reloading...")
					newCfg := config.NewConfig()
					if err := newCfg.LoadFromFile(fileName); err != nil {
						log.Error().Err(err).Msg("config reload: failed to load new config")
					} else {
						// Update existing config values
						*cfg = *newCfg
						lastMod = info.ModTime()
						log.Info().Msg("config reload: success")
					}
				}
			}
		}()
	}

	ms := monitor.NewMonitorService(cfg)

	// Start Telegram bot if enabled
	if cfg.TgBot.Enabled {
		if cfg.TgBot.Token == "" {
			log.Fatal().Msg("tgbot.enabled is true but tgbot.token is not set in configuration or token_file")
		}
		bot, err := tgbot.NewBot(&cfg.TgBot, ms)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to start Telegram bot")
		}
		ms.SetNotifier(bot)
		go bot.Run()
		log.Info().Str("json_file", cfg.TgBot.JsonFile).Msg("tgbot: started")
	} else {
		log.Debug().Msg("tgbot: disabled (set tgbot.enabled: true in configuration to activate)")
	}

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

	log.Debug().Msgf("warnings: %s", strings.Join(cfg.Monitor.Warnings, ", "))
	log.Debug().Msgf("listening at %s", cfg.Server.Url)

	http.HandleFunc(cfg.Server.Url, func(w http.ResponseWriter, r *http.Request) {
		apiHandler(w, r, ms)
	})

	if cfg.Server.Protocol == "https" {
		if cfg.Server.CertFile == "" || cfg.Server.KeyFile == "" {
			log.Fatal().Msg("HTTPS requested but cert_file or key_file is empty in configuration")
		}
		// Check if files exist and are readable
		if _, err := os.Stat(cfg.Server.CertFile); os.IsNotExist(err) {
			log.Fatal().Msgf("SSL certificate file not found: %s", cfg.Server.CertFile)
		}
		if _, err := os.Stat(cfg.Server.KeyFile); os.IsNotExist(err) {
			log.Fatal().Msgf("SSL key file not found: %s", cfg.Server.KeyFile)
		}
		err = http.ListenAndServeTLS(cfg.Server.Node(), cfg.Server.CertFile, cfg.Server.KeyFile, nil)
	} else {
		err = http.ListenAndServe(cfg.Server.Node(), nil)
	}

	if err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}
}


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
		if v.Type == "SDS_P1" {
			pm10 = v.Value
		} else if v.Type == "SDS_P2" {
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
