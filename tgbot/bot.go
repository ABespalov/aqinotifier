package tgbot

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/rs/zerolog/log"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
)

const BotVersion = "0.8.0a"

// chatState tracks what the bot is waiting for from a specific chat.
type chatState int

const (
	stateIdle            chatState = iota
	stateAwaitDeviceID             // waiting for user to type a device ID to subscribe
	stateAwaitPM10Green            // waiting for user to type PM10 green threshold value
	stateAwaitPM25Green            // waiting for user to type PM2.5 green threshold value
	stateAwaitPM10Yellow           // waiting for user to type PM10 yellow threshold value
	stateAwaitPM25Yellow           // waiting for user to type PM2.5 yellow threshold value
	stateAwaitDiff10               // waiting for user to type PM10 diff threshold
	stateAwaitDiff25               // waiting for user to type PM2.5 diff threshold
	stateAwaitDeviceName           // waiting for user to type a new device name
)

// Button labels for the persistent reply keyboard.
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
)

// Callback data commands for inline buttons.
const (
	cmdStatus         = "menu_status"
	cmdSettings       = "menu_settings"
	cmdHistory        = "menu_history"
	cmdCharts         = "menu_charts"
	cmdList           = "menu_list"
	cmdMonSettings    = "menu_settings"
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

// hPa to mmHg conversion factor.
const hPaToMmHg = 0.750064

// Bot wraps the Telegram bot API and the subscription store.
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

// NewBot creates and starts the Telegram bot using Telego library.
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

	// Long polling updates
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

// buildCommands builds the telego BotCommand slice for a given language.
func (b *Bot) buildCommands(lang string) []telego.BotCommand {
	return []telego.BotCommand{
		{Command: "start", Description: b.TLang(lang, "cmd_start_desc")},
		{Command: "list", Description: b.TLang(lang, "cmd_list_desc")},
		{Command: "status", Description: b.TLang(lang, "cmd_status_desc")},
		{Command: "help", Description: b.TLang(lang, "cmd_help_desc")},
		{Command: "lang", Description: b.TLang(lang, "cmd_lang_desc")},
	}
}

// registerCommands sets the localized command descriptions for all available languages.
// Adding a new translation file to lng/ is sufficient — no code changes needed.
func (b *Bot) registerCommands() {
	langs := AvailableLanguages()

	// Clear existing global and per-language commands first
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
			// English is the global default (no language code)
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

// syncUser detects the user's language from Telegram and updates the store if necessary.
func (b *Bot) syncUser(u *telego.User, chatID int64) bool {
	if u == nil {
		log.Warn().Int64("chat_id", chatID).Msg("tgbot: syncUser failed because User is nil")
		return false
	}
	detected := b.detectLang(u.LanguageCode)
	changed := b.store.SyncLanguage(chatID, u.LanguageCode, detected)
	if changed {
		log.Info().Int64("chat_id", chatID).Str("new", detected).Str("tg_code", u.LanguageCode).Msg("tgbot: language automatically synced from Telegram")
		b.updateCommandsForUser(chatID, detected)
	}
	return changed
}

// registerHandlers sets up the bot's reaction to commands and messages.
func (b *Bot) registerHandlers() {

	// Handlers for Telego v1.x (Handler signature: func(ctx *th.Context, update telego.Update) error)

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.setState(chatID, stateIdle)
		b.ensureReplyKeyboardRemoved(chatID)
		b.sendHelp(chatID)
		return nil
	}, th.CommandEqual("start"), th.CommandEqual("help"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdList(chatID)
		return nil
	}, th.CommandEqual("list"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdStatusMenu(chatID)
		return nil
	}, th.CommandEqual("status"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdUnsubscribeMenu(chatID)
		return nil
	}, th.CommandEqual("unsubscribe"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		text := update.Message.Text
		parts := strings.SplitN(text, " ", 2)
		if len(parts) < 2 {
			b.promptDeviceID(chatID)
			return nil
		}
		b.cmdSubscribeDevice(chatID, update.Message)
		return nil
	}, th.CommandEqual("subscribe"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdLangMenu(chatID)
		return nil
	}, th.CommandEqual("lang"))

	// Button text and state-based messages
	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		changed := b.syncUser(update.Message.From, chatID)
		if changed {
			log.Debug().Int64("chat_id", chatID).Msg("tgbot: language changed, refreshing menu")
			// If language changed, send a welcome message in the new language and refresh keyboard
			b.sendWithKeyboard(chatID, b.T(chatID, "msg_help"), b.mainKeyboard(chatID))
			return nil
		}
		b.handleMessage(update.Message)
		return nil
	}, th.AnyMessageWithText())

	// Callbacks
	b.handler.HandleCallbackQuery(func(_ *th.Context, query telego.CallbackQuery) error {
		b.syncUser(&query.From, query.Message.GetChat().ID)
		b.handleCallback(&query)
		return nil
	}, th.AnyCallbackQuery())
}

// mainKeyboard returns the persistent main menu keyboard.
// mainKeyboard returns the persistent main menu keyboard.
func (b *Bot) mainKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnStatus, IconStatus)).WithCallbackData(cmdStatus),
			tu.InlineKeyboardButton(b.T(chatID, btnSettings, IconSettings)).WithCallbackData(cmdSettings),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnHistory, IconHistory)).WithCallbackData(cmdHistory),
			tu.InlineKeyboardButton(b.T(chatID, btnCharts, IconChart)).WithCallbackData(cmdCharts),
		),
	)
}

// settingsKeyboard returns the keyboard for the settings menu.
func (b *Bot) settingsKeyboard(chatID int64) telego.ReplyMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSoundProfiles, IconLevels)).WithCallbackData(cmdSoundProfiles),
			tu.InlineKeyboardButton(b.T(chatID, btnSilentProfiles, IconDynamics)).WithCallbackData(cmdSilentProfiles),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnAQISettings, IconAQI)).WithCallbackData(cmdAQISettings),
			tu.InlineKeyboardButton(b.T(chatID, btnThresholds, IconThreshold)).WithCallbackData(cmdThresholdsMenu),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnList, IconList)).WithCallbackData(cmdList),
			tu.InlineKeyboardButton(b.T(chatID, btnResetDefaults, IconReset)).WithCallbackData(cmdResetSettings),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, IconBack)).WithCallbackData(cmdHelp),
		),
	)
}

func (b *Bot) resetDefaultsKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnYes, IconSuccess)).WithCallbackData("reset_defaults_yes"),
			tu.InlineKeyboardButton(b.T(chatID, btnNo, IconError)).WithCallbackData(cmdSettings),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnMonSettings, IconBack)).WithCallbackData(cmdMonSettings),
		),
	)
}

func (b *Bot) chartsMenuKeyboard(chatID int64, deviceID string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnChartAQI, IconAQI)).WithCallbackData(fmt.Sprintf("chart:aqi:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnChartPM, IconPM10)).WithCallbackData(fmt.Sprintf("chart:pm:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnChartTemp, IconTemp)).WithCallbackData(fmt.Sprintf("chart:temp:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnChartHum, IconHum)).WithCallbackData(fmt.Sprintf("chart:hum:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnChartPress, IconPress)).WithCallbackData(fmt.Sprintf("chart:press:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, IconBack)).WithCallbackData(cmdHelp),
		),
	)
}

func (b *Bot) thresholdsKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Green, IconGreen)).WithCallbackData(cmdPM25Green),
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Green, IconGreen)).WithCallbackData(cmdPM10Green),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Yellow, IconYellow)).WithCallbackData(cmdPM25Yellow),
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Yellow, IconYellow)).WithCallbackData(cmdPM10Yellow),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Diff, IconChart)).WithCallbackData(cmdPM25Diff),
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Diff, IconChart)).WithCallbackData(cmdPM10Diff),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSetByAQI, IconSetByAQI)).WithCallbackData("menu_aqi_cycle"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSettings, IconBack)).WithCallbackData(cmdSettings),
		),
	)
}

// subscriptionKeyboard returns the keyboard for the subscription management menu.
func (b *Bot) subscriptionKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSubscribe, IconSubscribe)).WithCallbackData("menu_subscribe"),
			tu.InlineKeyboardButton(b.T(chatID, btnUnsubscribe, IconUnsubscribe)).WithCallbackData("menu_unsubscribe"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnMonSettings, IconBack)).WithCallbackData(cmdMonSettings),
		),
	)
}

// Run starts the bot update loop (blocking).
func (b *Bot) Run() {
	b.handler.Start()
	log.Info().Msg("tgbot: bot is running and listening for updates")
	// Block here to keep the goroutine alive.
	// The actual processing is handled by b.handler in its own goroutine.
	select {}
}

// GetSubscribers returns all chat IDs subscribed to deviceID.
func (b *Bot) GetSubscribers(deviceID string) []int64 {
	return b.store.Subscribers(deviceID)
}

// GetUserSettings returns the personalized monitor settings for a chat.
func (b *Bot) GetUserSettings(chatID int64) *config.Monitor {
	return b.store.GetSettings(chatID, b.defaults)
}

// Notify delivers a single unified notification with appropriate styling based on events.
func (b *Bot) Notify(chatID int64, m *monitor.Measurement, alerts []monitor.AlertEvent, clears []monitor.AlertEvent, silent bool) {
	if len(alerts) == 0 && len(clears) == 0 {
		return
	}

	mcfg := b.GetUserSettings(chatID)
	var sb strings.Builder

	// 1. Header (Icon + Winner Title)
	var winnerID string
	maxPriority := -1
	allEvents := append([]monitor.AlertEvent{}, alerts...)
	allEvents = append(allEvents, clears...)

	for _, e := range allEvents {
		p := b.getEventPriority(e.ID)
		if p > maxPriority {
			maxPriority = p
			winnerID = e.ID
		}
	}
	icon, title := b.getEventHeader(chatID, winnerID)
	sb.WriteString(fmt.Sprintf("%s <b>%s</b>\n", icon, title))

	// 2. Timestamp
	t := m.Timestamp.Local()
	sb.WriteString(fmt.Sprintf("%s %s %s %s\n\n", IconDate, t.Format("02.01.2006"), IconTime, t.Format("15:04:05")))

	// 3. Event Texts
	hasAQIEvent := false
	for _, e := range allEvents {
		if strings.HasPrefix(e.ID, "aqi_") {
			hasAQIEvent = true
		}
		evtText := b.getEventDescription(chatID, e.ID)
		if evtText != "" {
			sb.WriteString(evtText + "\n")
		}
	}
	sb.WriteString("\n")

	// 4. AQI Section (if it's an AQI event, put it higher)
	aqiLine := b.formatAQILine(chatID, m, hasAQIEvent)
	if hasAQIEvent {
		sb.WriteString(aqiLine + "\n\n")
	}

	// 5. PM Data
	sb.WriteString(b.formatPMAlertLine(chatID, m, "PM2.5", mcfg, winnerID) + "\n\n")
	sb.WriteString(b.formatPMAlertLine(chatID, m, "PM10", mcfg, winnerID) + "\n\n")

	// 6. AQI Section (if it wasn't an AQI event, put it here)
	if !hasAQIEvent {
		sb.WriteString(aqiLine + "\n\n")
	}

	// 7. Weather Section
	sb.WriteString(b.formatWeatherLines(chatID, m))

	// 8. Footer
	sb.WriteString("\n\n" + b.formatFooter(chatID, m))

	params := tu.Message(tu.ID(chatID), sb.String()).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(b.mainKeyboard(chatID))
	params.DisableNotification = silent
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) getEventPriority(id string) int {
	switch {
	case strings.HasPrefix(id, "aqi_"):
		return 50
	case strings.HasPrefix(id, "vals-"):
		return 40
	case strings.HasPrefix(id, "val10-") || strings.HasPrefix(id, "val25-"):
		return 30
	case strings.HasPrefix(id, "diffs-"):
		return 20
	case strings.HasPrefix(id, "diff10-") || strings.HasPrefix(id, "diff25-"):
		return 10
	default:
		return 0
	}
}

func (b *Bot) getEventHeader(chatID int64, id string) (string, string) {
	// 1. Check for "Normal" (restored) state
	if id == "aqi_z1" || strings.HasSuffix(id, "-gd") {
		return IconGreen, b.T(chatID, "msg_norma")
	}

	// 2. Check for "Decrease" (not yet normal)
	if strings.HasSuffix(id, "-yd") || strings.HasSuffix(id, "-rd") {
		return IconYellow, b.T(chatID, "msg_decrease")
	}

	// 3. AQI specific icon
	if strings.HasPrefix(id, "aqi_") {
		return IconAQI, b.T(chatID, "msg_alert")
	}

	// 4. Dynamics (diff)
	if strings.HasPrefix(id, "diff") {
		icon := IconTrendUp
		if strings.HasSuffix(id, "d") {
			icon = IconTrendDown
		}
		return icon, b.T(chatID, "label_dynamics")
	}

	// Default
	return IconAlert, b.T(chatID, "msg_alert")
}

func (b *Bot) getEventDescription(chatID int64, id string) string {
	actUp := b.T(chatID, "alert_action_up")
	actDown := b.T(chatID, "alert_action_down")
	pm10 := b.T(chatID, "alert_pm10")
	pm25 := b.T(chatID, "alert_pm25")
	pms := b.T(chatID, "alert_pms")
	zAccG := b.T(chatID, "zone_acc_g")
	zAccY := b.T(chatID, "zone_acc_y")
	zAccR := b.T(chatID, "zone_acc_r")
	zPreG := b.T(chatID, "zone_pre_g")
	zPreY := b.T(chatID, "zone_pre_y")
	zPreR := b.T(chatID, "zone_pre_r")

	t := func(icon, act, pm, zone string) string {
		return fmt.Sprintf("%s <b>%s %s %s</b>", icon, act, pm, zone)
	}

	switch id {
	// PM10 transitions
	case "val10-yu":
		return t(IconTrendUp, actUp, pm10, zAccY)
	case "val10-ru":
		return t(IconTrendUp, actUp, pm10, zAccR)
	case "val10-yd":
		return t(IconTrendDown, actDown, pm10, zAccY)
	case "val10-gd":
		return t(IconTrendDown, actDown, pm10, zAccG)
	// PM2.5 transitions
	case "val25-yu":
		return t(IconTrendUp, actUp, pm25, zAccY)
	case "val25-ru":
		return t(IconTrendUp, actUp, pm25, zAccR)
	case "val25-yd":
		return t(IconTrendDown, actDown, pm25, zAccY)
	case "val25-gd":
		return t(IconTrendDown, actDown, pm25, zAccG)
	// Combined transitions
	case "vals-yu":
		return t(IconTrendUp, actUp, pms, zAccY)
	case "vals-ru":
		return t(IconTrendUp, actUp, pms, zAccR)
	case "vals-yd":
		return t(IconTrendDown, actDown, pms, zAccY)
	case "vals-gd":
		return t(IconTrendDown, actDown, pms, zAccG)
	// Dynamics PM10
	case "diff10-gu":
		return t(IconTrendUp, actUp, pm10, zPreG)
	case "diff10-yu":
		return t(IconTrendUp, actUp, pm10, zPreY)
	case "diff10-ru":
		return t(IconTrendUp, actUp, pm10, zAccR)
	case "diff10-gd":
		return t(IconTrendDown, actDown, pm10, zAccG)
	case "diff10-yd":
		return t(IconTrendDown, actDown, pm10, zAccY)
	case "diff10-rd":
		return t(IconTrendDown, actDown, pm10, zPreR)
	// Dynamics PM2.5
	case "diff25-gu":
		return t(IconTrendUp, actUp, pm25, zPreG)
	case "diff25-yu":
		return t(IconTrendUp, actUp, pm25, zPreY)
	case "diff25-ru":
		return t(IconTrendUp, actUp, pm25, zAccR)
	case "diff25-gd":
		return t(IconTrendDown, actDown, pm25, zAccG)
	case "diff25-yd":
		return t(IconTrendDown, actDown, pm25, zAccY)
	case "diff25-rd":
		return t(IconTrendDown, actDown, pm25, zPreR)
	// Dynamics combined
	case "diffs-gu":
		return t(IconTrendUp, actUp, pms, zPreG)
	case "diffs-yu":
		return t(IconTrendUp, actUp, pms, zPreY)
	case "diffs-ru":
		return t(IconTrendUp, actUp, pms, zAccR)
	case "diffs-gd":
		return t(IconTrendDown, actDown, pms, zAccG)
	case "diffs-yd":
		return t(IconTrendDown, actDown, pms, zAccY)
	case "diffs-rd":
		return t(IconTrendDown, actDown, pms, zPreR)
	}

	// AQI Notifications
	if strings.HasPrefix(id, "aqi_") {
		levelChar := strings.TrimPrefix(id, "aqi_")
		mcfg := b.GetUserSettings(chatID)
		std := strings.ToLower(mcfg.AQIStandard)
		name := b.T(chatID, "aqi_name_"+levelChar+"_"+std)
		if id == "aqi_z1" {
			return fmt.Sprintf("%s <b>%s</b>", IconSuccess, b.T(chatID, "alert_aqi_clean_short", name))
		}
		return fmt.Sprintf("%s <b>%s</b>", IconAlert, b.T(chatID, "alert_aqi_short", name, ""))
	}

	return ""
}

// ─── state helpers ────────────────────────────────────────────────────────────

func (b *Bot) getState(chatID int64) chatState {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	return b.states[chatID]
}

func (b *Bot) setState(chatID int64, s chatState) {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	b.states[chatID] = s
}

// ─── message handler ──────────────────────────────────────────────────────────

func (b *Bot) handleMessage(msg *telego.Message) {
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	if text == "" {
		return
	}

	// 1. Prepare robust matching for main menu buttons
	lang := b.store.GetLanguage(chatID)
	otherLang := "en"
	if lang == "en" {
		otherLang = "ru"
	}

	match := func(key string, icon string) bool {
		return text == b.T(chatID, key, icon) || text == b.TLang(otherLang, key, icon)
	}

	// 2. Check if the message is a command or a main menu button
	isCommand := strings.HasPrefix(text, "/")
	isMenuButton := match(btnList, IconList) ||
		match(btnCharts, IconChart) ||
		match(btnStatus, IconStatus) ||
		match(btnSettings, IconSettings) ||
		match(btnHistory, IconHistory) ||
		match(btnSubscribe, IconSubscribe) ||
		match(btnUnsubscribe, IconUnsubscribe) ||
		match(btnMainMenu, IconBack) ||
		match(btnInfo, IconInfo) ||
		strings.Contains(text, "Настройки") || strings.Contains(text, "Статус") ||
		strings.Contains(text, "Графики") || strings.Contains(text, "История") ||
		strings.Contains(text, "Settings") || strings.Contains(text, "Status") ||
		strings.Contains(text, "Charts") || strings.Contains(text, "History")

	if isCommand || isMenuButton {
		// Reset state if a command or button is pressed
		b.setState(chatID, stateIdle)

		// If a menu button was clicked, it means the old reply keyboard is still active.
		// We send a message with ReplyKeyboardRemove to clear it and switch to Inline-only mode.
		if isMenuButton {
			params := tu.Message(tu.ID(chatID), IconAQI+" AQI Notifier").
				WithReplyMarkup(tu.ReplyKeyboardRemove())
			_, _ = b.api.SendMessage(context.Background(), params)
		}

		switch {
		case text == "/start" || text == "/help":
			b.sendHelp(chatID)
		case match(btnList, IconList):
			b.cmdList(chatID)
		case match(btnCharts, IconChart):
			b.cmdChartsMenu(chatID)
		case match(btnStatus, IconStatus):
			b.cmdStatusMenu(chatID)
		case match(btnSettings, IconSettings):
			b.cmdSettings(chatID)
		case match(btnHistory, IconHistory):
			b.cmdHistoryMenu(chatID)
		case match(btnSubscribe, IconSubscribe):
			b.promptDeviceID(chatID)
		case match(btnUnsubscribe, IconUnsubscribe):
			b.cmdUnsubscribeMenu(chatID)
		case match(btnMainMenu, IconBack):
			b.sendHelp(chatID)
		}
		return
	}

	// 3. If not a command/button, handle based on state
	state := b.getState(chatID)
	switch state {
	case stateAwaitDeviceID:
		b.setState(chatID, stateIdle)
		b.cmdSubscribeDevice(chatID, msg)
		return
	case stateAwaitPM10Green, stateAwaitPM10Yellow, stateAwaitDiff10:
		b.handleThresholdUpdate(chatID, msg)
		return
	case stateAwaitPM25Green, stateAwaitPM25Yellow, stateAwaitDiff25:
		b.handleThresholdUpdate(chatID, msg)
		return
	case stateAwaitDeviceName:
		b.handleDeviceRename(chatID, msg)
		return
	}

	log.Debug().Int64("chat_id", chatID).Str("text", text).Str("lang", lang).Interface("state", state).Msg("tgbot: received message (idle)")
}

func (b *Bot) sendHelp(chatID int64) {
	b.clearLastPrompt(chatID)
	b.setState(chatID, stateIdle)
	// Pass AppVersion and BotVersion to the localized help message
	b.sendWithKeyboard(chatID, b.T(chatID, "msg_help", IconAQI, config.AppVersion, b.version, IconBullet), b.mainKeyboard(chatID))
}

// ─── commands ─────────────────────────────────────────────────────────────────

func (b *Bot) cmdLangMenu(chatID int64) {
	currentLang := b.store.GetLanguage(chatID)
	currentTemp := b.store.GetUnitTemp(chatID)
	currentPress := b.store.GetUnitPress(chatID)

	// Build language buttons dynamically from available translations
	langs := AvailableLanguages()
	var langBtns []telego.InlineKeyboardButton
	for _, lang := range langs {
		label := b.TLang(lang, "lang_"+lang)
		if lang == currentLang || (currentLang == "" && lang == "en") {
			label = IconSuccess + " " + label
		}
		langBtns = append(langBtns, tu.InlineKeyboardButton(label).WithCallbackData("lang_set:"+lang))
	}

	btnC := b.T(chatID, "unit_c")
	btnF := b.T(chatID, "unit_f")
	if currentTemp == "c" {
		btnC = IconSuccess + " " + btnC
	} else {
		btnF = IconSuccess + " " + btnF
	}

	btnMMHG := b.T(chatID, "unit_mmhg")
	btnHPA := b.T(chatID, "unit_hpa")
	if currentPress == "mmhg" {
		btnMMHG = IconSuccess + " " + btnMMHG
	} else {
		btnHPA = IconSuccess + " " + btnHPA
	}

	inlineKeyboard := tu.InlineKeyboard(
		tu.InlineKeyboardRow(langBtns...),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnC).WithCallbackData("unit_set:temp:c"),
			tu.InlineKeyboardButton(btnF).WithCallbackData("unit_set:temp:f"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnMMHG).WithCallbackData("unit_set:press:mmhg"),
			tu.InlineKeyboardButton(btnHPA).WithCallbackData("unit_set:press:hpa"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, IconBack)).WithCallbackData("menu_main"),
		),
	)

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_lang", IconLang)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(inlineKeyboard)
	_, _ = b.api.SendMessage(context.Background(), params)
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

func (b *Bot) promptDeviceID(chatID int64) {
	b.clearLastPrompt(chatID)
	b.setState(chatID, stateAwaitDeviceID)
	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_prompt_device", IconDevice)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(b.T(chatID, "btn_cancel", IconBack)).WithCallbackData("menu_list"),
			),
		))
	msg, err := b.api.SendMessage(context.Background(), params)
	if err == nil {
		b.setLastPrompt(chatID, msg.GetMessageID())
	}
}

func (b *Bot) cmdSubscribeDevice(chatID int64, msg *telego.Message) {
	b.clearLastPrompt(chatID)
	input := strings.TrimSpace(msg.Text)
	// If it's a command like "/subscribe 12345", extract the ID
	if strings.HasPrefix(input, "/subscribe") {
		parts := strings.SplitN(input, " ", 2)
		if len(parts) == 2 {
			input = parts[1]
		} else {
			input = ""
		}
	}
	deviceID := strings.TrimSpace(input)
	if deviceID == "" {
		b.promptDeviceID(chatID)
		return
	}

	// Validate: only digits
	for _, c := range deviceID {
		if c < '0' || c > '9' {
			b.sendWithKeyboard(chatID, b.T(chatID, "msg_invalid_device_id", IconAlert), b.mainKeyboard(chatID))
			return
		}
	}

	var text string
	if b.store.Subscribe(chatID, deviceID, b.defaults) {
		text = b.T(chatID, "msg_subscribed", IconSuccess, deviceID)
	} else {
		text = b.T(chatID, "msg_already_sub", IconInfo, deviceID)
	}
	b.sendWithKeyboard(chatID, text, b.subscriptionKeyboard(chatID))
}

func (b *Bot) cmdList(chatID int64) {
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		text := b.T(chatID, "msg_no_subs", IconEmpty, IconSubscribe)
		b.sendWithKeyboard(chatID, text, b.subscriptionKeyboard(chatID))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		mcfg := b.GetUserSettings(chatID)
		label := id
		if name, ok := mcfg.DeviceNames[id]; ok && name != "" {
			label = fmt.Sprintf("%s (%s)", name, id)
		}
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", IconStatus, label)).WithCallbackData(fmt.Sprintf("status:%s", id)),
			tu.InlineKeyboardButton(IconWrite).WithCallbackData(fmt.Sprintf("rename:%s", id)),
		})
	}

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_your_subs", IconList)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)

	b.sendSubscriptionKeyboard(chatID)
}

func (b *Bot) sendSubscriptionKeyboard(chatID int64) {
	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_manage_subs")).
		WithReplyMarkup(b.subscriptionKeyboard(chatID))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) cmdUnsubscribeMenu(chatID int64) {
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs", IconEmpty, IconSubscribe), b.subscriptionKeyboard(chatID))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s  %s", IconUnsubscribe, id)).WithCallbackData(fmt.Sprintf("unsub:%s", id)),
		})
	}

	// Add Back button
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, IconBack)).WithCallbackData("menu_settings"),
	})

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_unsub", IconDelete)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}
func (b *Bot) cmdStatusMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs", IconEmpty, IconSubscribe), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.sendWithKeyboard(chatID, b.formatDeviceStatus(chatID, devices[0]), b.mainKeyboard(chatID))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", IconStatus, id)).WithCallbackData(fmt.Sprintf("status:%s", id)),
		})
	}

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_device", IconDevice)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) cmdSettings(chatID int64) {
	b.clearLastPrompt(chatID)
	b.sendWithKeyboard(chatID, b.T(chatID, "msg_settings_title", IconSettings), b.settingsKeyboard(chatID))
}

func (b *Bot) cmdChartsMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs", IconEmpty, IconSubscribe), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_charts_menu", IconChart), b.chartsMenuKeyboard(chatID, devices[0]))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", IconStatus, id)).WithCallbackData(fmt.Sprintf("charts_dev:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, IconBack)).WithCallbackData("menu_main"),
	})

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_device", IconDevice)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) sendChartForDevice(chatID int64, deviceID string, chartType string) {
	hist := b.monitor.GetHistoryByDuration(deviceID, 24*time.Hour)
	if len(hist) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_history_empty", IconHistory, deviceID), b.chartsMenuKeyboard(chatID, deviceID))
		return
	}

	log.Debug().Msgf("Generating %s chart with width=%d, height=%d, fontSize=%.1f", chartType, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	buf, err := generateSingleChart(b, chatID, hist, chartType, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	if err != nil || buf == nil {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_charts", IconAlert, err), b.chartsMenuKeyboard(chatID, deviceID))
		return
	}

	nr := &bytesNamedReader{
		Reader: bytes.NewReader(buf),
		name:   fmt.Sprintf("chart_%s.png", chartType),
	}

	var typeName string
	switch chartType {
	case "pm":
		typeName = b.T(chatID, "chart_subject_pm")
	case "temp":
		typeName = b.T(chatID, "chart_subject_temp")
	case "hum":
		typeName = b.T(chatID, "chart_subject_hum")
	case "press":
		typeName = b.T(chatID, "chart_subject_press")
	case "aqi":
		typeName = b.T(chatID, "chart_subject_aqi")
	}

	params := tu.Photo(tu.ID(chatID), tu.File(nr)).
		WithCaption(b.T(chatID, "msg_chart_24h_title", IconChart, typeName, IconDevice, deviceID)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(b.chartsMenuKeyboard(chatID, deviceID))
	_, err = b.api.SendPhoto(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Msg("failed to send chart")
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_send_ch", IconAlert), b.chartsMenuKeyboard(chatID, deviceID))
	}
}

func (b *Bot) getAllAlerts(chatID int64) []struct {
	id   string
	name string
	loud bool
} {
	type alertDef struct {
		id   string
		pm   string // 10, 25, s
		act  string // u, d
		zone string // g, y, r
		loud bool
	}

	defs := []alertDef{
		// Sound (Level transitions)
		{"val25-yu", "25", "u", "y", true},
		{"val25-ru", "25", "u", "r", true},
		{"val25-yd", "25", "d", "y", true},
		{"val25-gd", "25", "d", "g", true},
		{"val10-yu", "10", "u", "y", true},
		{"val10-ru", "10", "u", "r", true},
		{"val10-yd", "10", "d", "y", true},
		{"val10-gd", "10", "d", "g", true},
		{"vals-yu", "s", "u", "y", true},
		{"vals-ru", "s", "u", "r", true},
		{"vals-yd", "s", "d", "y", true},
		{"vals-gd", "s", "d", "g", true},

		// Silent (Dynamics)
		{"diff25-gu", "25", "u", "g", false},
		{"diff10-gu", "10", "u", "g", false},
		{"diffs-gu", "s", "u", "g", false},
		{"diff25-gd", "25", "d", "g", false},
		{"diff10-gd", "10", "d", "g", false},
		{"diffs-gd", "s", "d", "g", false},
		{"diff25-yu", "25", "u", "y", false},
		{"diff10-yu", "10", "u", "y", false},
		{"diffs-yu", "s", "u", "y", false},
		{"diff25-yd", "25", "d", "y", false},
		{"diff10-yd", "10", "d", "y", false},
		{"diffs-yd", "s", "d", "y", false},
		{"diff25-ru", "25", "u", "r", false},
		{"diff10-ru", "10", "u", "r", false},
		{"diffs-ru", "s", "u", "r", false},
		{"diff25-rd", "25", "d", "r", false},
		{"diff10-rd", "10", "d", "r", false},
		{"diffs-rd", "s", "d", "r", false},
	}

	res := make([]struct {
		id   string
		name string
		loud bool
	}, len(defs))

	for i, d := range defs {
		// PM Type
		var pmLabel string
		switch d.pm {
		case "10":
			pmLabel = b.T(chatID, "alert_v10_short")
		case "25":
			pmLabel = b.T(chatID, "alert_v25_short")
		default:
			pmLabel = b.T(chatID, "alert_vs_short")
		}

		// Action icon
		actIcon := IconTrendUp
		if d.act == "d" {
			actIcon = IconTrendDown
		}
		// Zone icon
		var zoneIcon string
		switch d.zone {
		case "g":
			zoneIcon = IconGreen
		case "y":
			zoneIcon = IconYellow
		default:
			zoneIcon = IconRed
		}

		// Final name: PM2.5 📈 в 🟡
		res[i].id = d.id
		res[i].loud = d.loud
		res[i].name = fmt.Sprintf("%s %s %s %s", pmLabel, actIcon, b.T(chatID, "label_in"), zoneIcon)
	}

	return res
}

func (b *Bot) aqiSettingsKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	mcfg := b.GetUserSettings(chatID)
	std := mcfg.AQIStandard
	stdLabel := b.T(chatID, "standard_"+strings.ToLower(std))

	aqiAlerts := []string{"aqi_z1", "aqi_z2", "aqi_z3", "aqi_z4", "aqi_z5", "aqi_z6"}
	if std == "US" {
		aqiAlerts = append(aqiAlerts, "aqi_z7")
	}
	activeNotifications := make(map[string]bool)
	for _, n := range mcfg.Notifications {
		activeNotifications[n] = true
	}
	loudWarnings := make(map[string]bool)
	for _, w := range mcfg.Warnings {
		loudWarnings[w] = true
	}

	var rows [][]telego.InlineKeyboardButton
	// Row 1: Standard toggle
	iconFlag := IconFlagEU
	if strings.ToLower(std) == "us" {
		iconFlag = IconFlagUS
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, "btn_aqi_standard", stdLabel, iconFlag)).WithCallbackData("aqi_std_toggle"),
	})

	// Notification toggles
	stdLower := strings.ToLower(std)
	for _, id := range aqiAlerts {
		statusIcon := IconUnchecked
		if activeNotifications[id] {
			statusIcon = IconChecked
		}
		soundIcon := IconSilent
		soundLabel := "btn_without_sound"
		if loudWarnings[id] {
			soundIcon = IconLoud
			soundLabel = "btn_with_sound"
		}

		levelChar := strings.TrimPrefix(id, "aqi_")
		name := b.T(chatID, "aqi_name_"+levelChar+"_"+stdLower)

		var zoneIcon string
		if std == "US" {
			icons := map[string]string{"z1": IconGreen, "z2": IconYellow, "z3": IconOrange, "z4": IconRed, "z5": IconPurple, "z6": IconMaroon, "z7": IconBlack}
			zoneIcon = icons[levelChar]
		} else {
			icons := map[string]string{"z1": IconBlue, "z2": IconGreen, "z3": IconYellow, "z4": IconOrange, "z5": IconRed, "z6": IconMaroon}
			zoneIcon = icons[levelChar]
		}

		btnText := b.T(chatID, "alert_aqi_btn", name, zoneIcon)

		soundLabelStr := b.T(chatID, soundLabel)
		callbackData := fmt.Sprintf("aqi_sound:%s", id)
		if !activeNotifications[id] {
			soundLabelStr = b.T(chatID, "btn_inactive")
			soundIcon = ""
			callbackData = "none"
		}

		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", statusIcon, btnText)).
				WithCallbackData(fmt.Sprintf("aqi_toggle:%s", id)),
			tu.InlineKeyboardButton(strings.TrimSpace(fmt.Sprintf("%s %s", soundIcon, soundLabelStr))).
				WithCallbackData(callbackData),
		})
	}

	// Back button
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnMonSettings, IconBack)).WithCallbackData("menu_settings"),
	})

	return tu.InlineKeyboard(rows...)
}

func (b *Bot) cmdAqiMenu(chatID int64, editMsgID ...int) {
	text := b.T(chatID, "msg_aqi_settings", IconAQI)
	kb := b.aqiSettingsKeyboard(chatID)

	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(chatID), editMsgID[0], text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.EditMessageText(context.Background(), params)
	} else {
		params := tu.Message(tu.ID(chatID), text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.SendMessage(context.Background(), params)
	}
}

// cmdThresholdsMenu shows the thresholds submenu with manual settings.
func (b *Bot) cmdThresholdsMenu(chatID int64) {
	mcfg := b.GetUserSettings(chatID)
	text := b.T(chatID, "msg_thresholds_menu",
		IconThreshold,
		IconPM25, b.T(chatID, "label_pm25"),
		IconGreen, b.T(chatID, "label_zone_g"), mcfg.PM25Green,
		IconYellow, b.T(chatID, "label_zone_y"), mcfg.PM25Yellow,
		IconChart, b.T(chatID, "label_dynamics"), mcfg.PM25Diff,
		IconPM10, b.T(chatID, "label_pm10"),
		IconGreen, b.T(chatID, "label_zone_g"), mcfg.PM10Green,
		IconYellow, b.T(chatID, "label_zone_y"), mcfg.PM10Yellow,
		IconChart, b.T(chatID, "label_dynamics"), mcfg.PM10Diff)

	b.sendWithKeyboard(chatID, text, b.thresholdsKeyboard(chatID))
}

func (b *Bot) cmdAQICycleMenu(chatID int64, editMsgID ...int) {
	mcfg := b.GetUserSettings(chatID)

	text := b.T(chatID, "msg_aqi_cycle_menu",
		IconThreshold,
		IconPM25, b.T(chatID, "label_pm25"),
		IconGreen, b.T(chatID, "label_zone_g"), mcfg.PM25Green,
		IconYellow, b.T(chatID, "label_zone_y"), mcfg.PM25Yellow,
		IconPM10, b.T(chatID, "label_pm10"),
		IconGreen, b.T(chatID, "label_zone_g"), mcfg.PM10Green,
		IconYellow, b.T(chatID, "label_zone_y"), mcfg.PM10Yellow)

	getIcon := func(pmType string, val float64) (string, string) {
		var eu, us []float64
		if pmType == "PM10" {
			eu, us = sensor.BreakpointsEU10, sensor.BreakpointsUS10
		} else {
			eu, us = sensor.BreakpointsEU25, sensor.BreakpointsUS25
		}

		active := strings.ToLower(mcfg.AQIStandard)
		var std string
		var flag string
		found := false

		if active == "eu" {
			for _, v := range eu {
				if v == val {
					std = "EU"
					flag = IconFlagEU
					found = true
					break
				}
			}
			if !found {
				for _, v := range us {
					if v == val {
						std = "US"
						flag = IconFlagUS
						found = true
						break
					}
				}
			}
		} else {
			for _, v := range us {
				if v == val {
					std = "US"
					flag = IconFlagUS
					found = true
					break
				}
			}
			if !found {
				for _, v := range eu {
					if v == val {
						std = "EU"
						flag = IconFlagEU
						found = true
						break
					}
				}
			}
		}

		if found {
			_, level := sensor.CalculateValueAQI(val, pmType, std)
			return flag, b.getAQIIcon(level, std)
		}
		return IconWrite, ""
	}

	btnText := func(pmType, zoneIcon string, val float64) string {
		flag, levelIcon := getIcon(pmType, val)
		if levelIcon != "" {
			return fmt.Sprintf("%s%s ⇐ %s%s %.1f", pmType, zoneIcon, flag, levelIcon, val)
		}
		return fmt.Sprintf("%s%s ⇐ %s %.1f", pmType, zoneIcon, flag, val)
	}

	kb := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnText("PM2.5", IconGreen, mcfg.PM25Green)).WithCallbackData("aqi_cycle:PM2.5:green"),
			tu.InlineKeyboardButton(btnText("PM10", IconGreen, mcfg.PM10Green)).WithCallbackData("aqi_cycle:PM10:green"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnText("PM2.5", IconYellow, mcfg.PM25Yellow)).WithCallbackData("aqi_cycle:PM2.5:yellow"),
			tu.InlineKeyboardButton(btnText("PM10", IconYellow, mcfg.PM10Yellow)).WithCallbackData("aqi_cycle:PM10:yellow"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnThresholds, IconBack)).WithCallbackData(cmdThresholdsMenu),
		),
	)

	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(chatID), editMsgID[0], text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.EditMessageText(context.Background(), params)
	} else {
		b.sendWithKeyboard(chatID, text, kb)
	}
}

func (b *Bot) handleAQIThresholdCycle(chatID int64, data string, messageID int) {
	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		return
	}
	pmType := parts[1]
	thresholdType := parts[2]
	mcfg := b.GetUserSettings(chatID)

	var current float64
	if pmType == "PM10" {
		if thresholdType == "green" {
			current = mcfg.PM10Green
		} else {
			current = mcfg.PM10Yellow
		}
	} else {
		if thresholdType == "green" {
			current = mcfg.PM25Green
		} else {
			current = mcfg.PM25Yellow
		}
	}

	active := strings.ToLower(mcfg.AQIStandard)
	var activeList, otherList []float64

	if pmType == "PM10" {
		if active == "eu" {
			activeList, otherList = sensor.BreakpointsEU10, sensor.BreakpointsUS10
		} else {
			activeList, otherList = sensor.BreakpointsUS10, sensor.BreakpointsEU10
		}
	} else {
		if active == "eu" {
			activeList, otherList = sensor.BreakpointsEU25, sensor.BreakpointsUS25
		} else {
			activeList, otherList = sensor.BreakpointsUS25, sensor.BreakpointsEU25
		}
	}

	findNext := func(val float64, list []float64) (float64, bool) {
		for _, v := range list {
			if v > val {
				return v, true
			}
		}
		return 0, false
	}

	findNearestOrFirst := func(val float64, list []float64) float64 {
		for _, v := range list {
			if v >= val {
				return v
			}
		}
		return list[0]
	}

	var nextVal float64
	isInActive := false
	for _, v := range activeList {
		if v == current {
			isInActive = true
			break
		}
	}

	if isInActive {
		nv, ok := findNext(current, activeList)
		if ok {
			nextVal = nv
		} else {
			nextVal = otherList[0]
		}
	} else {
		isInOther := false
		for _, v := range otherList {
			if v == current {
				isInOther = true
				break
			}
		}

		if isInOther {
			nv, ok := findNext(current, otherList)
			if ok {
				nextVal = nv
			} else {
				nextVal = activeList[0]
			}
		} else {
			nextVal = findNearestOrFirst(current, activeList)
		}
	}

	if pmType == "PM10" {
		if thresholdType == "green" {
			mcfg.PM10Green = nextVal
		} else {
			mcfg.PM10Yellow = nextVal
		}
	} else {
		if thresholdType == "green" {
			mcfg.PM25Green = nextVal
		} else {
			mcfg.PM25Yellow = nextVal
		}
	}
	b.store.UpdateSettings(chatID, mcfg)
	b.cmdAQICycleMenu(chatID, messageID)
}

func (b *Bot) getAQIIcon(level sensor.AQILevel, standard string) string {
	if strings.ToUpper(standard) == "US" {
		switch level {
		case sensor.LevelGood:
			return IconGreen
		case sensor.LevelModerate:
			return IconYellow
		case sensor.LevelSlightlyUnhealthy:
			return IconOrange
		case sensor.LevelUnhealthy:
			return IconRed
		case sensor.LevelVeryUnhealthy:
			return IconPurple
		case sensor.LevelHazardous:
			return IconMaroon
		case sensor.LevelExtremelyHazardous:
			return IconBlack
		}
	} else {
		switch level {
		case sensor.LevelGood:
			return IconBlue
		case sensor.LevelModerate:
			return IconGreen
		case sensor.LevelSlightlyUnhealthy:
			return IconYellow
		case sensor.LevelUnhealthy:
			return IconOrange
		case sensor.LevelVeryUnhealthy:
			return IconRed
		case sensor.LevelHazardous, sensor.LevelExtremelyHazardous:
			return IconMaroon
		}
	}
	return IconUnknown
}

func (b *Bot) handleThresholdUpdate(chatID int64, msg *telego.Message) {
	b.clearLastPrompt(chatID)
	text := strings.TrimSpace(msg.Text)

	var val float64
	_, err := fmt.Sscanf(text, "%f", &val)
	if err != nil {
		b.setState(chatID, stateIdle)
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_number", IconAlert), b.thresholdsKeyboard(chatID))
		return
	}

	mcfg := b.GetUserSettings(chatID)
	var title string
	var oldVal float64
	state := b.getState(chatID)
	b.setState(chatID, stateIdle)

	switch state {
	case stateAwaitPM10Green:
		oldVal = mcfg.PM10Green
		mcfg.PM10Green = val
		title = b.T(chatID, "msg_threshold_title_fmt", b.T(chatID, "msg_threshold_label"), b.T(chatID, "label_pm10"), b.T(chatID, "label_zone_g"), IconGreen, b.T(chatID, "label_zone_suffix"))
	case stateAwaitPM10Yellow:
		if val < mcfg.PM10Green {
			b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_yellow", IconAlert), b.thresholdsKeyboard(chatID))
			return
		}
		oldVal = mcfg.PM10Yellow
		mcfg.PM10Yellow = val
		title = b.T(chatID, "msg_threshold_title_fmt", b.T(chatID, "msg_threshold_label"), b.T(chatID, "label_pm10"), b.T(chatID, "label_zone_y"), IconYellow, b.T(chatID, "label_zone_suffix"))
	case stateAwaitDiff10:
		oldVal = mcfg.PM10Diff
		mcfg.PM10Diff = val
		title = b.T(chatID, "msg_threshold_diff_title_fmt", b.T(chatID, "label_pm10"), b.T(chatID, "label_dynamics"), IconPM10)
	case stateAwaitPM25Green:
		oldVal = mcfg.PM25Green
		mcfg.PM25Green = val
		title = b.T(chatID, "msg_threshold_title_fmt", b.T(chatID, "msg_threshold_label"), b.T(chatID, "label_pm25"), b.T(chatID, "label_zone_g"), IconGreen, b.T(chatID, "label_zone_suffix"))
	case stateAwaitPM25Yellow:
		if val < mcfg.PM25Green {
			b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_yellow", IconAlert), b.thresholdsKeyboard(chatID))
			return
		}
		oldVal = mcfg.PM25Yellow
		mcfg.PM25Yellow = val
		title = b.T(chatID, "msg_threshold_title_fmt", b.T(chatID, "msg_threshold_label"), b.T(chatID, "label_pm25"), b.T(chatID, "label_zone_y"), IconYellow, b.T(chatID, "label_zone_suffix"))
	case stateAwaitDiff25:
		oldVal = mcfg.PM25Diff
		mcfg.PM25Diff = val
		title = b.T(chatID, "msg_threshold_diff_title_fmt", b.T(chatID, "label_pm25"), b.T(chatID, "label_dynamics"), IconPM25)
	default:
		return
	}
	// Send confirmation without keyboard
	b.sendWithKeyboard(chatID, b.T(chatID, "msg_threshold_upd", IconSuccess, title, oldVal, val), nil)
	// Then show the menu
	b.cmdThresholdsMenu(chatID)
}

func (b *Bot) cmdHistoryMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs", IconSubscribe), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.cmdDeviceHistory(chatID, devices[0])
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", IconHistory, id)).WithCallbackData(fmt.Sprintf("history:%s", id)),
		})
	}

	// Add Back button
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, IconBack)).WithCallbackData("menu_main"),
	})

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_history")).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) clearLastPrompt(chatID int64) {
	b.lastPromptsMu.Lock()
	msgID, ok := b.lastPrompts[chatID]
	delete(b.lastPrompts, chatID)
	b.lastPromptsMu.Unlock()

	if ok && msgID != 0 {
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{
			ChatID:    tu.ID(chatID),
			MessageID: msgID,
		})
	}
}

func (b *Bot) setLastPrompt(chatID int64, msgID int) {
	b.lastPromptsMu.Lock()
	b.lastPrompts[chatID] = msgID
	b.lastPromptsMu.Unlock()
}

func (b *Bot) notificationSettingsKeyboard(chatID int64, silent bool) *telego.InlineKeyboardMarkup {
	mcfg := b.GetUserSettings(chatID)
	allAlerts := b.getAllAlerts(chatID)

	activeNotifications := make(map[string]bool)
	for _, n := range mcfg.Notifications {
		activeNotifications[n] = true
	}

	activeWarnings := make(map[string]bool)
	for _, w := range mcfg.Warnings {
		activeWarnings[w] = true
	}

	var rows [][]telego.InlineKeyboardButton
	for _, a := range allAlerts {
		if a.loud != !silent {
			continue
		}

		statusIcon := IconUnchecked
		if activeNotifications[a.id] {
			statusIcon = IconChecked
		}

		soundIcon := IconSilent
		soundLabel := "btn_without_sound"
		if activeWarnings[a.id] {
			soundIcon = IconLoud
			soundLabel = "btn_with_sound"
		}

		soundLabelStr := b.T(chatID, soundLabel)
		callbackData := fmt.Sprintf("toggle_sound:%s:%t", a.id, silent)
		if !activeNotifications[a.id] {
			soundLabelStr = b.T(chatID, "btn_inactive")
			soundIcon = ""
			callbackData = "none"
		}

		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", statusIcon, a.name)).
				WithCallbackData(fmt.Sprintf("toggle:%s:%t", a.id, silent)),
			tu.InlineKeyboardButton(strings.TrimSpace(fmt.Sprintf("%s %s", soundIcon, soundLabelStr))).
				WithCallbackData(callbackData),
		})
	}

	// Add Back button
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, "btn_mon_settings", IconBack)).WithCallbackData("menu_settings"),
	})

	return tu.InlineKeyboard(rows...)
}

func (b *Bot) cmdSoundMenu(chatID int64, silent bool, editMsgID ...int) {
	var sb strings.Builder
	if silent {
		sb.WriteString(b.T(chatID, "msg_silent_alerts", IconDynamics))
	} else {
		sb.WriteString(b.T(chatID, "msg_loud_alerts", IconLevels))
	}
	sb.WriteString(b.T(chatID, "msg_sound_settings"))

	kb := b.notificationSettingsKeyboard(chatID, silent)

	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(chatID), editMsgID[0], sb.String()).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.EditMessageText(context.Background(), params)
	} else {
		params := tu.Message(tu.ID(chatID), sb.String()).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.SendMessage(context.Background(), params)
	}
}

func (b *Bot) promptThreshold(chatID int64, param, zone string) {
	mcfg := b.GetUserSettings(chatID)
	var currentVal float64

	// Base parts
	var pmLabel string
	var zoneLabel string
	var zoneIcon string

	switch param {
	case "PM10":
		pmLabel = b.T(chatID, "label_pm10")
		switch zone {
		case "green":
			b.setState(chatID, stateAwaitPM10Green)
			zoneLabel = b.T(chatID, "label_zone_g")
			zoneIcon = IconGreen
			currentVal = mcfg.PM10Green
		case "yellow":
			b.setState(chatID, stateAwaitPM10Yellow)
			zoneLabel = b.T(chatID, "label_zone_y")
			zoneIcon = IconYellow
			currentVal = mcfg.PM10Yellow
		case "diff":
			b.setState(chatID, stateAwaitDiff10)
			zoneLabel = b.T(chatID, "msg_threshold_diff_title")
			zoneIcon = IconTrendUp
			currentVal = mcfg.PM10Diff
		}
	case "PM2.5":
		pmLabel = b.T(chatID, "label_pm25")
		switch zone {
		case "green":
			b.setState(chatID, stateAwaitPM25Green)
			zoneLabel = b.T(chatID, "label_zone_g")
			zoneIcon = IconGreen
			currentVal = mcfg.PM25Green
		case "yellow":
			b.setState(chatID, stateAwaitPM25Yellow)
			zoneLabel = b.T(chatID, "label_zone_y")
			zoneIcon = IconYellow
			currentVal = mcfg.PM25Yellow
		case "diff":
			b.setState(chatID, stateAwaitDiff25)
			zoneLabel = b.T(chatID, "msg_threshold_diff_title")
			zoneIcon = IconTrendUp
			currentVal = mcfg.PM25Diff
		}
	}

	// For "diff", we want "Threshold PM10: Dynamics (%)"
	// For zones: "🟢 Порог PM10: Зеленая зона"
	var title string
	if zone == "diff" {
		title = b.T(chatID, "msg_threshold_diff_title_fmt", pmLabel, zoneLabel, zoneIcon)
	} else {
		title = b.T(chatID, "msg_threshold_title_fmt",
			b.T(chatID, "msg_threshold_label"),
			pmLabel,
			zoneLabel,
			zoneIcon,
			b.T(chatID, "label_zone_suffix"))
	}

	text := b.T(chatID, "msg_threshold_prompt", title, currentVal)
	b.clearLastPrompt(chatID)
	msg, err := b.api.SendMessage(context.Background(), tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(b.T(chatID, "btn_cancel", IconBack)).WithCallbackData("menu_thresholds"),
			),
		)))
	if err == nil {
		b.setLastPrompt(chatID, msg.GetMessageID())
	}
}

func (b *Bot) handleCallback(cq *telego.CallbackQuery) {
	_ = b.api.AnswerCallbackQuery(context.Background(), tu.CallbackQuery(cq.ID))

	data := cq.Data
	if cq.Message == nil {
		return
	}
	chatID := cq.Message.GetChat().ID

	switch {
	case data == "none":
		return
	case data == "menu_reset_defaults":
		b.cmdResetConfirm(chatID)
		return

	case data == "menu_main":
		b.sendHelp(chatID)
	case data == "menu_settings":
		b.cmdSettings(chatID)
	case data == "menu_status":
		b.cmdStatusMenu(chatID)
	case data == "menu_charts":
		b.cmdChartsMenu(chatID)
	case data == "menu_history":
		b.cmdHistoryMenu(chatID)
	case data == "menu_list":
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.cmdList(chatID)
	case data == "menu_thresholds":
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.cmdThresholdsMenu(chatID)
	case data == "menu_aqi_cycle":
		b.cmdAQICycleMenu(chatID, cq.Message.GetMessageID())
	case data == "menu_aqi_thresholds":
		b.cmdAQICycleMenu(chatID, cq.Message.GetMessageID())
	case data == "reset_defaults":
		b.cmdResetConfirm(chatID)
	case data == "reset_defaults_yes":
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.cmdResetExecute(chatID)
	case data == "menu_sound":
		b.cmdSoundMenu(chatID, false)
	case data == "menu_silent":
		b.cmdSoundMenu(chatID, true)
	case data == "menu_subscribe":
		b.promptDeviceID(chatID)
	case data == "menu_unsubscribe":
		b.cmdUnsubscribeMenu(chatID)
	case data == "menu_aqi":
		b.cmdAqiMenu(chatID, cq.Message.GetMessageID())
	case data == "aqi_std_toggle":
		mcfg := b.GetUserSettings(chatID)
		if mcfg.AQIStandard == "EU" {
			mcfg.AQIStandard = "US"
		} else {
			mcfg.AQIStandard = "EU"
		}
		b.store.UpdateSettings(chatID, mcfg)
		b.cmdAqiMenu(chatID, cq.Message.GetMessageID())
	case strings.HasPrefix(data, "aqi_toggle:"):
		id := strings.TrimPrefix(data, "aqi_toggle:")
		mcfg := b.GetUserSettings(chatID)
		found := -1
		for i, n := range mcfg.Notifications {
			if n == id {
				found = i
				break
			}
		}
		if found >= 0 {
			mcfg.Notifications = append(mcfg.Notifications[:found], mcfg.Notifications[found+1:]...)
		} else {
			mcfg.Notifications = append(mcfg.Notifications, id)
		}
		b.store.UpdateSettings(chatID, mcfg)
		b.cmdAqiMenu(chatID, cq.Message.GetMessageID())
	case strings.HasPrefix(data, "aqi_sound:"):
		id := strings.TrimPrefix(data, "aqi_sound:")
		mcfg := b.GetUserSettings(chatID)
		found := -1
		for i, w := range mcfg.Warnings {
			if w == id {
				found = i
				break
			}
		}
		if found >= 0 {
			mcfg.Warnings = append(mcfg.Warnings[:found], mcfg.Warnings[found+1:]...)
		} else {
			mcfg.Warnings = append(mcfg.Warnings, id)
		}
		b.store.UpdateSettings(chatID, mcfg)
		b.cmdAqiMenu(chatID, cq.Message.GetMessageID())
	case strings.HasPrefix(data, "charts_dev:"):
		deviceID := strings.TrimPrefix(data, "charts_dev:")
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_charts_menu", IconChart), b.chartsMenuKeyboard(chatID, deviceID))

	case strings.HasPrefix(data, "pm_set:"):
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			b.promptThreshold(chatID, parts[1], parts[2])
		}
	case strings.HasPrefix(data, "unsub:"):
		deviceID := strings.TrimPrefix(data, "unsub:")
		if b.store.Unsubscribe(chatID, deviceID) {
			params := tu.EditMessageText(tu.ID(chatID), cq.Message.GetMessageID(),
				b.T(chatID, "msg_unsubscribed", IconUnsubscribe, deviceID)).
				WithParseMode(telego.ModeHTML)
			_, _ = b.api.EditMessageText(context.Background(), params)
		}

	case strings.HasPrefix(data, "aqi_cycle:"):
		b.handleAQIThresholdCycle(chatID, data, cq.Message.GetMessageID())
		return

	case strings.HasPrefix(data, "unit_set:"):
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			cat := parts[1]
			val := parts[2]
			if cat == "temp" {
				b.store.SetUnitTemp(chatID, val)
			} else {
				b.store.SetUnitPress(chatID, val)
			}
			_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{
				ChatID:    tu.ID(chatID),
				MessageID: cq.Message.GetMessageID(),
			})
			b.cmdLangMenu(chatID)
		}

	case strings.HasPrefix(data, "lang_set:"):
		lang := strings.TrimPrefix(data, "lang_set:")
		b.store.SetLanguage(chatID, lang)
		log.Debug().Int64("chat_id", chatID).Str("to", lang).Msg("tgbot: language changed via menu")
		b.updateCommandsForUser(chatID, lang)
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{
			ChatID:    tu.ID(chatID),
			MessageID: cq.Message.GetMessageID(),
		})
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_help"), b.mainKeyboard(chatID))

	case strings.HasPrefix(data, "toggle:"):
		parts := strings.Split(data, ":")
		if len(parts) < 3 {
			return
		}
		alertID := parts[1]
		silent := parts[2] == "true"

		mcfg := b.GetUserSettings(chatID)
		found := -1
		for i, n := range mcfg.Notifications {
			if n == alertID {
				found = i
				break
			}
		}
		if found >= 0 {
			mcfg.Notifications = append(mcfg.Notifications[:found], mcfg.Notifications[found+1:]...)
		} else {
			mcfg.Notifications = append(mcfg.Notifications, alertID)
		}
		b.store.UpdateSettings(chatID, mcfg)
		b.cmdSoundMenu(chatID, silent, cq.Message.GetMessageID())

	case strings.HasPrefix(data, "toggle_sound:"):
		parts := strings.Split(data, ":")
		if len(parts) < 3 {
			return
		}
		alertID := parts[1]
		silent := parts[2] == "true"

		mcfg := b.GetUserSettings(chatID)
		found := -1
		for i, w := range mcfg.Warnings {
			if w == alertID {
				found = i
				break
			}
		}
		if found >= 0 {
			mcfg.Warnings = append(mcfg.Warnings[:found], mcfg.Warnings[found+1:]...)
		} else {
			mcfg.Warnings = append(mcfg.Warnings, alertID)
		}
		b.store.UpdateSettings(chatID, mcfg)
		b.cmdSoundMenu(chatID, silent, cq.Message.GetMessageID())

	case strings.HasPrefix(data, "rename:"):
		deviceID := strings.TrimPrefix(data, "rename:")
		b.cmdRename(chatID, deviceID)
		return

	case data == "rename_cancel":
		b.setState(chatID, stateIdle)
		b.renameIDMu.Lock()
		delete(b.renameIDs, chatID)
		b.renameIDMu.Unlock()
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.cmdList(chatID)
		return

	case strings.HasPrefix(data, "status:"):
		deviceID := strings.TrimPrefix(data, "status:")
		// Back button returns to device selection list
		b.sendWithKeyboard(chatID, b.formatDeviceStatus(chatID, deviceID), tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(b.T(chatID, "btn_rename", IconWrite)).WithCallbackData(fmt.Sprintf("rename:%s", deviceID)),
				tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, IconBack)).WithCallbackData("menu_status"),
			),
		))

	case strings.HasPrefix(data, "chart:"):
		parts := strings.SplitN(strings.TrimPrefix(data, "chart:"), ":", 2)
		if len(parts) == 2 {
			chartType := parts[0]
			deviceID := parts[1]
			b.sendChartForDevice(chatID, deviceID, chartType)
		}

	case strings.HasPrefix(data, "history:"):
		deviceID := strings.TrimPrefix(data, "history:")
		b.cmdDeviceHistory(chatID, deviceID, cq.Message.GetMessageID())
	}
}

// ─── formatting ───────────────────────────────────────────────────────────────

func (b *Bot) formatDeviceStatus(chatID int64, deviceID string) string {
	m := b.monitor.LastMeasurement(deviceID)
	if m == nil {
		return b.T(chatID, "status_no_data", IconUnknown, b.formatDeviceID(chatID, deviceID))
	}
	return b.formatMeasurement(chatID, m)
}

func (b *Bot) cmdRename(chatID int64, deviceID string) {
	b.setState(chatID, stateAwaitDeviceName)
	b.renameIDMu.Lock()
	b.renameIDs[chatID] = deviceID
	b.renameIDMu.Unlock()

	text := b.T(chatID, "msg_rename_prompt", IconWrite, deviceID)
	keyboard := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, "msg_rename_cancel")).WithCallbackData("rename_cancel"),
		),
	)
	b.sendWithKeyboard(chatID, text, keyboard)
}

func (b *Bot) handleDeviceRename(chatID int64, msg *telego.Message) {
	b.setState(chatID, stateIdle)
	b.renameIDMu.Lock()
	deviceID := b.renameIDs[chatID]
	delete(b.renameIDs, chatID)
	b.renameIDMu.Unlock()

	if deviceID == "" {
		return
	}

	name := strings.TrimSpace(msg.Text)
	if name == "" {
		return
	}

	mcfg := b.GetUserSettings(chatID)
	if mcfg.DeviceNames == nil {
		mcfg.DeviceNames = make(map[string]string)
	}
	mcfg.DeviceNames[deviceID] = name
	b.store.UpdateSettings(chatID, mcfg)

	text := b.T(chatID, "msg_device_renamed", name, deviceID)
	_, _ = b.api.SendMessage(context.Background(), tu.Message(tu.ID(chatID), text).WithParseMode(telego.ModeHTML))
	b.cmdList(chatID)
}

func (b *Bot) formatDeviceID(chatID int64, deviceID string) string {
	mcfg := b.GetUserSettings(chatID)
	name, ok := mcfg.DeviceNames[deviceID]
	if ok && name != "" {
		return fmt.Sprintf("%s (<code>%s</code>)", name, deviceID)
	}
	return fmt.Sprintf("<code>%s</code>", deviceID)
}

type bytesNamedReader struct {
	*bytes.Reader
	name string
}

func (b *bytesNamedReader) Name() string {
	return b.name
}

func (b *Bot) cmdDeviceHistory(chatID int64, deviceID string, msgID ...int) {
	// If triggered from an inline button, delete the menu message to keep chat clean
	if len(msgID) > 0 {
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{
			ChatID:    tu.ID(chatID),
			MessageID: msgID[0],
		})
	}

	hist := b.monitor.GetHistory(deviceID)
	if len(hist) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_history_empty", deviceID), b.mainKeyboard(chatID))
		return
	}

	log.Debug().Msgf("Generating charts with width=%d, height=%d, fontSize=%.1f", b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	buffers, err := generateCharts(b, chatID, hist, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	if err != nil {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_charts", IconAlert, err), b.mainKeyboard(chatID))
		return
	}

	var media []telego.InputMedia
	for i, buf := range buffers {
		nr := &bytesNamedReader{
			Reader: bytes.NewReader(buf),
			name:   fmt.Sprintf("chart_%d.png", i),
		}

		photo := &telego.InputMediaPhoto{
			Type:  "photo",
			Media: tu.File(nr),
		}

		media = append(media, photo)
	}

	params := tu.MediaGroup(tu.ID(chatID), media...)
	_, err = b.api.SendMediaGroup(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Msg("failed to send history charts")
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_send_ch", IconAlert), b.mainKeyboard(chatID))
		return
	}

	// Send footer message after the media group
	footer := b.T(chatID, "msg_history_footer", IconHistory, b.sys.ValuesInRam, IconDevice, deviceID)
	b.sendWithKeyboard(chatID, footer, b.mainKeyboard(chatID))
}

func (b *Bot) cmdResetConfirm(chatID int64) {
	d := b.defaults
	std := strings.ToLower(d.AQIStandard)
	stdLabel := b.T(chatID, "standard_"+std)

	iconFlag := IconFlagEU
	if std == "us" {
		iconFlag = IconFlagUS
	}

	unitT := b.T(chatID, "unit_"+b.cfg.DefaultUnitTemp)
	unitP := b.T(chatID, "unit_"+b.cfg.DefaultUnitPress)

	// Notifications list
	var alertsSB strings.Builder
	allAlerts := b.getAllAlerts(chatID)

	// Map defaults for easy lookup
	defNotifications := make(map[string]bool)
	for _, n := range d.Notifications {
		defNotifications[n] = true
	}
	defWarnings := make(map[string]bool)
	for _, w := range d.Warnings {
		defWarnings[w] = true
	}

	for _, a := range allAlerts {
		if defNotifications[a.id] {
			icon := "•"
			if defWarnings[a.id] {
				icon = IconLoud
			} else {
				icon = IconSilent
			}
			alertsSB.WriteString(fmt.Sprintf("%s %s\n", icon, a.name))
		}
	}

	details := b.T(chatID, "msg_reset_confirm_details",
		IconPM25, b.T(chatID, "label_pm25"), b.T(chatID, "msg_threshold_label"), d.PM25Green, d.PM25Yellow, b.T(chatID, "label_dynamics"), d.PM25Diff,
		IconPM10, b.T(chatID, "label_pm10"), b.T(chatID, "msg_threshold_label"), d.PM10Green, d.PM10Yellow, b.T(chatID, "label_dynamics"), d.PM10Diff,
		IconAQI, stdLabel, iconFlag,
		IconSettings, unitT, unitP,
		IconAlert, alertsSB.String())

	text := b.T(chatID, "msg_reset_confirm", IconThreshold) + details
	b.sendWithKeyboard(chatID, text, b.resetDefaultsKeyboard(chatID))
}

func (b *Bot) cmdResetExecute(chatID int64) {
	b.store.ResetSettings(chatID, b.defaults)

	mcfg := b.store.GetSettings(chatID, b.defaults)
	text := b.T(chatID, "msg_reset_done", IconSuccess,
		IconPM25, b.T(chatID, "label_pm25"), b.T(chatID, "label_zone_g"), mcfg.PM25Green, b.T(chatID, "label_zone_y"), mcfg.PM25Yellow, b.T(chatID, "label_dynamics"), mcfg.PM25Diff,
		IconPM10, b.T(chatID, "label_pm10"), b.T(chatID, "label_zone_g"), mcfg.PM10Green, b.T(chatID, "label_zone_y"), mcfg.PM10Yellow, b.T(chatID, "label_dynamics"), mcfg.PM10Diff)

	b.sendWithKeyboard(chatID, text, b.settingsKeyboard(chatID))
}

func (b *Bot) convertTemp(celsius float64, chatID int64) float64 {
	unit := b.store.GetUnitTemp(chatID)
	if unit == "f" {
		return celsius*1.8 + 32
	}
	return celsius
}

func (b *Bot) convertPress(hpa float64, chatID int64) float64 {
	unit := b.store.GetUnitPress(chatID)
	if unit == "mmhg" {
		return hpa * hPaToMmHg
	}
	return hpa
}

func (b *Bot) unitTempLabel(chatID int64) string {
	unit := b.store.GetUnitTemp(chatID)
	if unit == "f" {
		return "°F"
	}
	return "°C"
}

func (b *Bot) unitPressLabel(chatID int64) string {
	unit := b.store.GetUnitPress(chatID)
	if unit == "mmhg" {
		return b.T(chatID, "msg_unit_mmhg")
	}
	return b.T(chatID, "unit_hpa")
}

func (b *Bot) formatMeasurement(chatID int64, m *monitor.Measurement) string {
	var sb strings.Builder
	t := m.Timestamp.Local()
	sb.WriteString(b.T(chatID, "msg_status_header",
		IconStatus, IconDate, t.Format("02.01.2006"),
		IconTime, t.Format("15:04:05"),
	))

	sb.WriteString(strings.TrimSpace(b.formatAQILine(chatID, m, true)))
	sb.WriteString("\n\n")
	sb.WriteString(b.formatPMStatusLine(chatID, m, "PM2.5"))
	sb.WriteString("\n\n")
	sb.WriteString(b.formatPMStatusLine(chatID, m, "PM10"))
	sb.WriteString("\n\n")
	sb.WriteString(strings.TrimSpace(b.formatWeatherLines(chatID, m)))
	sb.WriteString("\n\n")
	sb.WriteString(strings.TrimSpace(b.formatFooter(chatID, m)))

	return sb.String()
}

func (b *Bot) formatAQILine(chatID int64, m *monitor.Measurement, bold bool) string {
	mcfg := b.GetUserSettings(chatID)
	var aqi float64
	var level sensor.AQILevel
	std := strings.ToLower(mcfg.AQIStandard)
	if mcfg.AQIStandard == "US" {
		aqi, level = sensor.CalculateUS_AQI(m.PM25, m.PM10)
	} else {
		aqi, level = sensor.CalculateEU_AQI(m.PM25, m.PM10)
	}

	aqiIcon := b.getAQIIcon(level, mcfg.AQIStandard)
	levelChar := fmt.Sprintf("z%d", level)
	aqiName := b.T(chatID, "aqi_name_"+levelChar+"_"+std)
	iconFlag := IconFlagEU
	if std == "us" {
		iconFlag = IconFlagUS
	}
	flag := b.T(chatID, "flag_"+std, iconFlag)

	if bold {
		return b.T(chatID, "msg_status_aqi", aqiIcon, aqi, aqiName, flag)
	}
	// Simplified non-bold AQI
	return fmt.Sprintf("%s AQI: %.1f — %s %s", aqiIcon, aqi, aqiName, flag)
}

func (b *Bot) formatPMStatusLine(chatID int64, m *monitor.Measurement, pmType string) string {
	mcfg := b.GetUserSettings(chatID)
	var val, prev float64
	var pcent *float64
	var icon, label string
	var g, y float64

	if pmType == "PM10" {
		val, prev = m.PM10, m.PM10Prev
		pcent = m.PM10Diff
		icon, label = IconPM10, b.T(chatID, "label_pm10")
		g, y = mcfg.PM10Green, mcfg.PM10Yellow
	} else {
		val, prev = m.PM25, m.PM25Prev
		pcent = m.PM25Diff
		icon, label = IconPM25, b.T(chatID, "label_pm25")
		g, y = mcfg.PM25Green, mcfg.PM25Yellow
	}

	getZoneIcon := func(v float64) string {
		if v <= g {
			return IconGreen
		}
		if v <= y {
			return IconYellow
		}
		return IconRed
	}

	formatDiff := func() string {
		if pcent == nil || *pcent == 0 {
			return "    " + IconTrendFlat + " " + b.T(chatID, "msg_no_changes")
		}
		trendIcon := IconTrendUp
		if *pcent < 0 {
			trendIcon = IconTrendDown
		}
		return strings.TrimSuffix(b.T(chatID, "msg_status_diff", trendIcon, *pcent, prev, val), "\n\n")
	}

	line := b.T(chatID, "msg_status_pm", icon, label, val, b.T(chatID, "chart_unit_pm"), getZoneIcon(val))
	return strings.TrimSpace(line) + "\n" + formatDiff()
}

func (b *Bot) formatPMAlertLine(chatID int64, m *monitor.Measurement, pmType string, mcfg *config.Monitor, winnerID string) string {
	var val, prev, diff float64
	var pcentVal float64
	var icon, label string
	var g, y float64

	if pmType == "PM10" {
		val, prev, diff = m.PM10, m.PM10Prev, m.PM10-m.PM10Prev
		if m.PM10Diff != nil {
			pcentVal = *m.PM10Diff
		}
		icon, label = IconPM10, b.T(chatID, "label_pm10")
		g, y = mcfg.PM10Green, mcfg.PM10Yellow
	} else {
		val, prev, diff = m.PM25, m.PM25Prev, m.PM25-m.PM25Prev
		if m.PM25Diff != nil {
			pcentVal = *m.PM25Diff
		}
		icon, label = IconPM25, b.T(chatID, "label_pm25")
		g, y = mcfg.PM25Green, mcfg.PM25Yellow
	}

	getZoneIcon := func(v float64) string {
		if v <= g {
			return IconGreen
		}
		if v <= y {
			return IconYellow
		}
		return IconRed
	}

	trendIcon := IconTrendUp
	if diff < 0 {
		trendIcon = IconTrendDown
	} else if diff == 0 {
		trendIcon = IconTrendFlat
	}

	// Calculate relevant threshold
	var threshold float64
	var thresholdIcon string
	// Check if this PM was the cause of zone transition
	isTransition := false
	if strings.Contains(winnerID, "vals-") {
		isTransition = true
	} else if pmType == "PM10" && strings.Contains(winnerID, "10-") {
		isTransition = true
	} else if pmType == "PM2.5" && strings.Contains(winnerID, "25-") {
		isTransition = true
	}

	if isTransition {
		// Crossed Green?
		if (prev <= g && val > g) || (prev > g && val <= g) {
			threshold, thresholdIcon = g, IconGreen
		} else {
			threshold, thresholdIcon = y, IconYellow
		}
	} else {
		// Dynamics within zone, find nearest above
		if val <= g {
			threshold, thresholdIcon = g, IconGreen
		} else {
			threshold, thresholdIcon = y, IconYellow
		}
	}

	unit := b.T(chatID, "chart_unit_pm")
	pcentStr := fmt.Sprintf("%+.2f%%", pcentVal)
	newValStr := fmt.Sprintf("%.2f %s", val, unit)
	if strings.HasPrefix(winnerID, "diff") {
		pcentStr = "<b>" + pcentStr + "</b>"
	} else if strings.HasPrefix(winnerID, "val") {
		newValStr = "<b>" + newValStr + "</b>"
	}

	boundaryLabel := b.T(chatID, "msg_boundary")

	// ░ **PM2.5** -1.25 мкг/м³ (-43.86%)
	//    📉 2.85 → **1.60 мкг/м³** 🟢 Граница 🟢 10.0
	zoneIcon := ""
	if isTransition {
		zoneIcon = getZoneIcon(val) + " "
	}

	return fmt.Sprintf("%s <b>%s</b> %+.2f %s (%s)\n    %s %.2f → %s %s%s %s %.1f",
		icon, label, diff, unit, pcentStr,
		trendIcon, prev, newValStr, zoneIcon, boundaryLabel, thresholdIcon, threshold)
}

func (b *Bot) formatWeatherLines(chatID int64, m *monitor.Measurement) string {
	var sb strings.Builder
	if m.Temperature != 0 {
		val := b.convertTemp(m.Temperature, chatID)
		sb.WriteString(strings.TrimSuffix(b.T(chatID, "msg_status_temp", IconTemp, b.T(chatID, "msg_temp"), val, b.unitTempLabel(chatID)), "\n"))
	}
	if m.Humidity != 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(strings.TrimSuffix(b.T(chatID, "msg_status_hum", IconHum, b.T(chatID, "msg_hum"), m.Humidity), "\n"))
		if m.Temperature != 0 {
			dp := CalcDewPoint(m.Temperature, m.Humidity)
			dpConverted := b.convertTemp(dp, chatID)
			sb.WriteString("\n")
			sb.WriteString(strings.TrimSuffix(b.T(chatID, "msg_status_dew_point", IconDewPoint, b.T(chatID, "msg_dew_point"), dpConverted, b.unitTempLabel(chatID)), "\n"))
		}
	}
	if m.Pressure != 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		val := b.convertPress(m.Pressure, chatID)
		sb.WriteString(strings.TrimSuffix(b.T(chatID, "msg_status_press", IconPress, b.T(chatID, "msg_press"), val, b.unitPressLabel(chatID)), "\n"))
	}
	return sb.String()
}

func (b *Bot) formatFooter(chatID int64, m *monitor.Measurement) string {
	return strings.TrimSpace(b.T(chatID, "msg_status_device", IconDevice, b.formatDeviceID(chatID, m.DeviceID)))
}

// Stop shuts down the bot update loop and long polling.
func (b *Bot) Stop() {
	log.Info().Msg("tgbot: stopping bot...")
	b.stopFunc()
	b.handler.Stop()
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (b *Bot) sendWithKeyboard(chatID int64, text string, markup telego.ReplyMarkup) {
	params := tu.Message(tu.ID(chatID), text).
		WithReplyMarkup(markup).
		WithParseMode(telego.ModeHTML)

	_, _ = b.api.SendMessage(context.Background(), params)
}

// ensureReplyKeyboardRemoved sends a one-time message to clear the old persistent keyboard.
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

func (b *Bot) SyncDB() {
	if b.store != nil {
		b.store.SyncDB()
	}
}
