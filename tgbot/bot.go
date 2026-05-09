package tgbot

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/rs/zerolog/log"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/monitor"
)

const BotVersion = "0.9.7a"

type chatState int

const (
	stateIdle chatState = iota
	stateAwaitDeviceID
	stateAwaitPM10Green
	stateAwaitPM25Green
	stateAwaitPM10Yellow
	stateAwaitPM25Yellow
	stateAwaitDiff10
	stateAwaitDiff25
	stateAwaitDeviceName
)

const (
	btnList           = "btn_list"
	btnStatus         = "btn_status"
	btnSettings       = "btn_settings"
	btnHistory        = "btn_history"
	btnSubscribe      = "btn_subscribe"
	btnUnsubscribe    = "btn_unsubscribe"
	btnMainMenu       = "btn_main_menu"
	btnThresholds     = "btn_thresholds"
	btnPM10Green      = "btn_pm10_green"
	btnPM25Green      = "btn_pm25_green"
	btnPM10Yellow     = "btn_pm10_yellow"
	btnPM25Yellow     = "btn_pm25_yellow"
	btnPM10Diff       = "btn_pm10_diff"
	btnPM25Diff       = "btn_pm25_diff"
	btnCharts         = "btn_charts"
	btnSetByAQI       = "btn_set_pm_by_aqi"
	btnChartPM        = "btn_chart_pm"
	btnChartTemp      = "btn_chart_temp"
	btnChartHum       = "btn_chart_hum"
	btnChartPress     = "btn_chart_press"
	btnResetDefaults  = "btn_reset_defaults"
	btnInfo           = "btn_info"
	btnMonSettings    = "btn_mon_settings"
	btnAQISettings    = "btn_aqi_settings"
	btnChartAQI       = "btn_chart_aqi"
	btnSoundProfiles  = "btn_sound_profiles"
	btnSilentProfiles = "btn_silent_profiles"
	btnYes            = "btn_yes"
	btnNo             = "btn_no"
	btnBack           = "btn_back"
)

const (
	cmdStatus         = "menu_status"
	cmdSettings       = "menu_settings"
	cmdHistory        = "menu_history"
	cmdCharts         = "menu_charts"
	cmdList           = "menu_list"
	cmdThresholdsMenu = "menu_thresholds"
	cmdAQISettings    = "menu_aqi"
	cmdResetSettings  = "menu_reset_defaults"
	cmdHelp           = "menu_main"
	cmdSoundProfiles  = "menu_sound"
	cmdSilentProfiles = "menu_silent"
	cmdPM10Green      = "pm_set:PM10:green"
	cmdPM10Yellow     = "pm_set:PM10:yellow"
	cmdPM25Green      = "pm_set:PM2.5:green"
	cmdPM25Yellow     = "pm_set:PM2.5:yellow"
	cmdPM10Diff       = "pm_set:PM10:diff"
	cmdPM25Diff       = "pm_set:PM2.5:diff"
)

const hPaToMmHg = 0.750064

type Bot struct {
	api     *telego.Bot
	handler *th.BotHandler
	store   *Store
	monitor *monitor.MonitorService
	cfg     *config.TgBot
	sys     *config.System

	stateMu sync.Mutex
	states  map[int64]chatState

	stopFunc context.CancelFunc
	defaults *config.Monitor
	version  string

	lastPromptsMu sync.Mutex
	lastPrompts   map[int64]int

	renameIDMu sync.Mutex
	renameIDs  map[int64]string
}

func NewBot(fullCfg *config.Config, monitorDefaults *config.Monitor, ms *monitor.MonitorService) (*Bot, error) {
	cfg := &fullCfg.TgBot
	var opts []telego.BotOption
	if cfg.Debug {
		opts = append(opts, telego.WithDefaultDebugLogger())
	}

	api, err := telego.NewBot(cfg.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("tgbot: failed to create bot: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	updates, err := api.UpdatesViaLongPolling(ctx, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("tgbot: failed to start updates: %w", err)
	}

	handler, err := th.NewBotHandler(api, updates)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("tgbot: failed to create handler: %w", err)
	}

	b := &Bot{
		api:         api,
		handler:     handler,
		store:       NewStore(cfg.JsonFile, cfg.DefaultUnitTemp, cfg.DefaultUnitPress),
		monitor:     ms,
		cfg:         cfg,
		sys:         &fullCfg.System,
		states:      make(map[int64]chatState),
		stopFunc:    cancel,
		defaults:    monitorDefaults,
		version:     BotVersion,
		lastPrompts: make(map[int64]int),
		renameIDs:   make(map[int64]string),
	}

	b.registerHandlers()
	self, err := api.GetMe(context.Background())
	if err != nil {
		return nil, fmt.Errorf("tgbot: failed to get bot info: %w", err)
	}
	log.Info().Str("username", self.Username).Msg("tgbot: bot authorized (telego)")

	b.registerCommands()
	return b, nil
}

func (b *Bot) buildCommands(lang string) []telego.BotCommand {
	return []telego.BotCommand{
		{Command: "start", Description: b.TLang(lang, "cmd_start_desc")},
		{Command: "list", Description: b.TLang(lang, "cmd_list_desc")},
		{Command: "status", Description: b.TLang(lang, "cmd_status_desc")},
		{Command: "help", Description: b.TLang(lang, "cmd_help_desc")},
		{Command: "lang", Description: b.TLang(lang, "cmd_lang_desc")},
	}
}

func (b *Bot) registerCommands() {
	langs := AvailableLanguages()

	_ = b.api.DeleteMyCommands(context.Background(), &telego.DeleteMyCommandsParams{})
	for _, lang := range langs {
		if lang != "en" {
			_ = b.api.DeleteMyCommands(context.Background(), &telego.DeleteMyCommandsParams{
				LanguageCode: lang,
			})
		}
	}

	for _, lang := range langs {
		cmds := b.buildCommands(lang)
		var params *telego.SetMyCommandsParams
		if lang == "en" {

			params = &telego.SetMyCommandsParams{Commands: cmds}
		} else {
			params = &telego.SetMyCommandsParams{Commands: cmds, LanguageCode: lang}
		}
		if err := b.api.SetMyCommands(context.Background(), params); err != nil {
			log.Error().Err(err).Str("lang", lang).Msg("tgbot: failed to set commands")
		} else {
			log.Info().Str("lang", lang).Msg("tgbot: commands registered")
		}
	}
}

func (b *Bot) Run() {
	b.handler.Start()
	log.Info().Msg("tgbot: bot is running and listening for updates")

	select {}
}

func (b *Bot) GetSubscribers(deviceID string) []int64 {
	return b.store.Subscribers(deviceID)
}

func (b *Bot) GetUserSettings(chatID int64) *config.Monitor {
	return b.store.GetSettings(chatID, b.defaults)
}

func (b *Bot) Notify(chatID int64, m *monitor.Measurement, alerts []monitor.AlertEvent, clears []monitor.AlertEvent, silent bool) {
	if len(alerts) == 0 && len(clears) == 0 {
		return
	}

	mcfg := b.GetUserSettings(chatID)
	var sb strings.Builder

	allEvents := append([]monitor.AlertEvent{}, alerts...)
	allEvents = append(allEvents, clears...)

	var winnerID string
	maxPriority := -1
	for _, e := range allEvents {
		p := b.getEventPriority(e.ID)
		if p > maxPriority {
			maxPriority = p
			winnerID = e.ID
		}
	}

	icon, title := b.getEventHeader(chatID, winnerID)
	sb.WriteString(fmt.Sprintf("%s <b>%s</b>\n", icon, title))

	t := m.Timestamp.Local()
	sb.WriteString(b.T(chatID, "msg_status_time", map[string]interface{}{"date": t, "time": t}) + "\n")

	winnerDesc := b.getEventDescription(chatID, winnerID)
	if winnerDesc != "" {
		sb.WriteString(winnerDesc + "\n")
	}

	for _, e := range allEvents {
		if e.ID == winnerID {
			continue
		}
		evtText := b.getEventDescription(chatID, e.ID)
		if evtText != "" {
			sb.WriteString(evtText + "\n")
		}
	}
	sb.WriteString("\n")

	hasAQIEvent := strings.HasPrefix(winnerID, "aqi_")
	aqiLine := b.formatAQILine(chatID, m, hasAQIEvent)
	sb.WriteString(aqiLine + "\n\n")

	sb.WriteString(b.formatPMAlertLine(chatID, m, "PM2.5", mcfg, winnerID) + "\n\n")
	sb.WriteString(b.formatPMAlertLine(chatID, m, "PM10", mcfg, winnerID) + "\n\n")

	sb.WriteString(b.formatWeatherLines(chatID, m))

	sb.WriteString("\n\n" + b.formatFooter(chatID, m))

	params := tu.Message(tu.ID(chatID), sb.String()).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(b.mainKeyboard(chatID))
	params.DisableNotification = silent
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) sendHelp(chatID int64) {
	b.clearLastPrompt(chatID)
	b.setState(chatID, stateIdle)

	b.sendWithKeyboard(chatID, b.T(chatID, "msg_help", map[string]interface{}{
		"bot_version": BotVersion,
	}), b.mainKeyboard(chatID))
}

func (b *Bot) updateCommandsForUser(chatID int64, lang string) {
	if lang == "" {
		lang = "en"
	}
	cmds := b.buildCommands(lang)
	log.Debug().Int64("chat_id", chatID).Str("lang", lang).Msg("tgbot: updating commands for user scope")
	err := b.api.SetMyCommands(context.Background(), &telego.SetMyCommandsParams{
		Commands:     cmds,
		Scope:        &telego.BotCommandScopeChat{Type: "chat", ChatID: tu.ID(chatID)},
		LanguageCode: lang,
	})
	if err != nil {
		log.Error().Err(err).Int64("chat_id", chatID).Msg("tgbot: failed to set per-user commands")
	}
}

type bytesNamedReader struct {
	*bytes.Reader
	name string
}

func (b *bytesNamedReader) Name() string {
	return b.name
}

func (b *Bot) Stop() {
	log.Info().Msg("tgbot: stopping bot...")
	b.stopFunc()
	b.handler.Stop()
}

func (b *Bot) ensureReplyKeyboardRemoved(chatID int64) {
	params := tu.Message(tu.ID(chatID), "🌬️ AQI Notifier").
		WithReplyMarkup(tu.ReplyKeyboardRemove())
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) SetDB(db *sql.DB) {
	if b.store != nil {
		b.store.SetDB(db)
	}
}
