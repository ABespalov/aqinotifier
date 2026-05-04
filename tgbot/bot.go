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
)

const BotVersion = "0.6.6a"

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
	btnInfo           = "btn_info"
	btnThresholds     = "btn_thresholds"
	btnSoundProfiles  = "btn_sound_profiles"
	btnSilentProfiles = "btn_silent_profiles"
	btnPM10Green      = "btn_pm10_green"
	btnPM25Green      = "btn_pm25_green"
	btnPM10Yellow     = "btn_pm10_yellow"
	btnPM25Yellow     = "btn_pm25_yellow"
	btnPM10Diff       = "btn_pm10_diff"
	btnPM25Diff       = "btn_pm25_diff"
	btnCharts         = "btn_charts"
	btnChartPM        = "btn_chart_pm"
	btnChartTemp      = "btn_chart_temp"
	btnChartHum       = "btn_chart_hum"
	btnChartPress     = "btn_chart_press"
	btnResetDefaults  = "btn_reset_defaults"
)

// Icons used throughout the bot UI.
const (
	iconList        = "📋"  // Lists / Subscriptions
	iconStatus      = "📊"  // Status / Data
	iconSettings    = "⚙️" // Settings
	iconHistory     = "📈"  // History
	iconSubscribe   = "➕"  // Add
	iconUnsubscribe = "❌"  // Delete / Unsubscribe
	iconBack        = "🔙"  // Back
	iconDate        = "📅"  // Date
	iconTime        = "🕐"  // Time
	iconPM10        = "💨"  // PM10 particles
	iconPM25        = "░"  // PM2.5 particles
	iconTemp        = "🌡"  // Temperature
	iconHum         = "💧"  // Humidity
	iconPress       = "🗿"  // Pressure
	iconAlert       = "🚨"  // Alert (Critical)
	iconWarning     = "⚠️" // Warning (Medium level)
	iconSuccess     = "✅"  // Success / Normal
	iconGreen       = "🟢"  // Clean zone
	iconYellow      = "🟡"  // Level decrease
	iconDewPoint    = "🌫️" // Dew point
	iconInfo        = "ℹ️" // Information
	iconEmpty       = "📭"  // Empty
	iconDelete      = "🗑"  // Deletion
	iconDevice      = "📡"  // Device / ID
	iconLang        = "🌐"  // Language
	iconUnknown     = "❓"  // No data
	iconTrendUp     = "🔺"  // Trend Up (growth)
	iconTrendDown   = "▼"  // Trend Down (decrease)
	iconTrendFlat   = "—"  // Flat (no change)
	iconBullet      = "•"  // List marker
	iconLoud        = "🔊"  // With sound
	iconSilent      = "🔕"  // Without sound
	iconThreshold   = "⚖️" // Thresholds
	iconChecked     = "✅"  // Enabled
	iconUnchecked   = "❌"  // Disabled
	iconReset       = "🔄"  // Reset to defaults
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
		api:      api,
		handler:  handler,
		store:    NewStore(cfg.JsonFile),
		monitor:  ms,
		cfg:      cfg,
		sys:      &fullCfg.System,
		states:   make(map[int64]chatState),
		stopFunc: cancel,
		defaults:      monitorDefaults,
		version:       BotVersion,
		lastPrompts:   make(map[int64]int),
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

	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.setState(chatID, stateIdle)
		b.ensureReplyKeyboardRemoved(chatID)
		b.sendHelp(chatID)
		return nil
	}, th.CommandEqual("start"), th.CommandEqual("help"))

	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdList(chatID)
		return nil
	}, th.CommandEqual("list"))

	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdStatusMenu(chatID)
		return nil
	}, th.CommandEqual("status"))

	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdUnsubscribeMenu(chatID)
		return nil
	}, th.CommandEqual("unsubscribe"))

	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
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

	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdLangMenu(chatID)
		return nil
	}, th.CommandEqual("lang"))

	// Button text and state-based messages
	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
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
	b.handler.HandleCallbackQuery(func(ctx *th.Context, query telego.CallbackQuery) error {
		b.syncUser(&query.From, query.Message.GetChat().ID)
		b.handleCallback(&query)
		return nil
	}, th.AnyCallbackQuery())
}

// mainKeyboard returns the persistent main menu keyboard.
func (b *Bot) mainKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnStatus, iconStatus)).WithCallbackData("menu_status"),
			tu.InlineKeyboardButton(b.T(chatID, btnSettings, iconSettings)).WithCallbackData("menu_settings"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnHistory, iconHistory)).WithCallbackData("menu_history"),
			tu.InlineKeyboardButton(b.T(chatID, btnCharts, iconHistory)).WithCallbackData("menu_charts"),
		),
	)
}

// settingsKeyboard returns the keyboard for the settings menu.
func (b *Bot) settingsKeyboard(chatID int64) telego.ReplyMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSoundProfiles, iconLoud)).WithCallbackData("menu_sound"),
			tu.InlineKeyboardButton(b.T(chatID, btnSilentProfiles, iconSilent)).WithCallbackData("menu_silent"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnList, iconList)).WithCallbackData("menu_list"),
			tu.InlineKeyboardButton(b.T(chatID, btnThresholds, iconThreshold)).WithCallbackData("menu_thresholds"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnResetDefaults, iconReset)).WithCallbackData("reset_defaults"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, iconBack)).WithCallbackData("menu_main"),
		),
	)
}

func (b *Bot) resetDefaultsKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, "btn_yes")).WithCallbackData("reset_defaults_yes"),
			tu.InlineKeyboardButton(b.T(chatID, "btn_no")).WithCallbackData("menu_settings"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, "btn_mon_settings", iconBack)).WithCallbackData("menu_settings"),
		),
	)
}

// thresholdsKeyboard returns the keyboard for the thresholds submenu.

func (b *Bot) chartsMenuKeyboard(chatID int64, deviceID string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnChartPM, iconPM10)).WithCallbackData(fmt.Sprintf("chart:pm:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnChartTemp, iconTemp)).WithCallbackData(fmt.Sprintf("chart:temp:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnChartHum, iconHum)).WithCallbackData(fmt.Sprintf("chart:hum:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnChartPress, iconPress)).WithCallbackData(fmt.Sprintf("chart:press:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, iconBack)).WithCallbackData("menu_main"),
		),
	)
}
func (b *Bot) thresholdsKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Green, iconPM10)).WithCallbackData("pm_set:PM10:green"),
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Green, iconPM25)).WithCallbackData("pm_set:PM2.5:green"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Yellow, iconPM10)).WithCallbackData("pm_set:PM10:yellow"),
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Yellow, iconPM25)).WithCallbackData("pm_set:PM2.5:yellow"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Diff, iconTrendUp)).WithCallbackData("pm_set:PM10:diff"),
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Diff, iconTrendUp)).WithCallbackData("pm_set:PM2.5:diff"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnResetDefaults, iconReset)).WithCallbackData("reset_defaults"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, "btn_mon_settings", iconBack)).WithCallbackData("menu_settings"),
		),
	)
}

// subscriptionKeyboard returns the keyboard for the subscription management menu.
func (b *Bot) subscriptionKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSubscribe, iconSubscribe)).WithCallbackData("menu_subscribe"),
			tu.InlineKeyboardButton(b.T(chatID, btnUnsubscribe, iconUnsubscribe)).WithCallbackData("menu_unsubscribe"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, "btn_mon_settings", iconBack)).WithCallbackData("menu_settings"),
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

// SendWarning delivers a warning message to a specific subscriber.
func (b *Bot) SendWarning(chatID int64, deviceID string, m *monitor.Measurement, messages []string, silent bool) {
	text := b.formatWarning(chatID, deviceID, m, messages)
	params := tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML)
	params.DisableNotification = silent
	_, _ = b.api.SendMessage(context.Background(), params)
}

// SendClear delivers a "values returned to normal" notification to a specific subscriber.
func (b *Bot) SendClear(chatID int64, deviceID string, m *monitor.Measurement, messages []string) {
	text := b.formatClear(chatID, deviceID, m, messages)
	params := tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML)
	_, _ = b.api.SendMessage(context.Background(), params)
}

// Notify delivers a single unified notification with appropriate styling.
func (b *Bot) Notify(chatID int64, deviceID string, m *monitor.Measurement, alertMessages []string, clearMessages []string, silent bool) {
	var text string
	if len(clearMessages) > 0 {
		// If anything returned to normal, it's a "Success/Green" vibe.
		// We append alert messages (like sharp decrease) if they exist.
		allMsgs := append([]string{}, clearMessages...)
		allMsgs = append(allMsgs, alertMessages...)

		text = b.formatGeneric(chatID, deviceID, m, iconGreen, b.T(chatID, "msg_norma"), allMsgs)
	} else if len(alertMessages) > 0 {
		// Determine icon based on message content
		icon := iconAlert
		title := b.T(chatID, "msg_alert")

		isDecrease := true
		for _, msg := range alertMessages {
			lmsg := strings.ToLower(msg)
			// If at least one message is about growth or exceed, use Alert.
			// Checking localized versions for "decrease" / "снижение"
			if !strings.Contains(lmsg, strings.ToLower(b.T(chatID, "msg_decrease"))) &&
				!strings.Contains(lmsg, "снижение") && !strings.Contains(lmsg, "decrease") {
				isDecrease = false
				break
			}
		}

		if isDecrease {
			icon = iconYellow
			title = b.T(chatID, "msg_decrease")
		}

		text = b.formatGeneric(chatID, deviceID, m, icon, title, alertMessages)
	} else {
		return
	}

	params := tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(b.mainKeyboard(chatID))
	params.DisableNotification = silent
	_, _ = b.api.SendMessage(context.Background(), params)
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
	isMenuButton := match(btnList, iconList) ||
		match(btnCharts, iconHistory) ||
		match(btnStatus, iconStatus) ||
		match(btnSettings, iconSettings) ||
		match(btnHistory, iconHistory) ||
		match(btnSubscribe, iconSubscribe) ||
		match(btnUnsubscribe, iconUnsubscribe) ||
		match(btnMainMenu, iconBack) ||
		match(btnInfo, iconInfo) ||
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
			params := tu.Message(tu.ID(chatID), "🌬️ AQI Notifier").
				WithReplyMarkup(tu.ReplyKeyboardRemove())
			_, _ = b.api.SendMessage(context.Background(), params)
		}

		switch {
		case text == "/start" || text == "/help":
			b.sendHelp(chatID)
		case match(btnList, iconList):
			b.cmdList(chatID)
		case match(btnCharts, iconHistory):
			b.cmdChartsMenu(chatID)
		case match(btnStatus, iconStatus):
			b.cmdStatusMenu(chatID)
		case match(btnSettings, iconSettings):
			b.cmdSettings(chatID)
		case match(btnHistory, iconHistory):
			b.cmdHistoryMenu(chatID)
		case match(btnSubscribe, iconSubscribe):
			b.promptDeviceID(chatID)
		case match(btnUnsubscribe, iconUnsubscribe):
			b.cmdUnsubscribeMenu(chatID)
		case match(btnMainMenu, iconBack):
			b.sendHelp(chatID)
		case match(btnInfo, iconInfo):
			b.cmdInfo(chatID)
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
	}

	log.Debug().Int64("chat_id", chatID).Str("text", text).Str("lang", lang).Interface("state", state).Msg("tgbot: received message (idle)")
}

func (b *Bot) sendHelp(chatID int64) {
	b.clearLastPrompt(chatID)
	b.setState(chatID, stateIdle)
	// Pass AppVersion and BotVersion to the localized help message
	b.sendWithKeyboard(chatID, b.T(chatID, "msg_help", config.AppVersion, b.version), b.mainKeyboard(chatID))
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
			label = "✅ " + label
		}
		langBtns = append(langBtns, tu.InlineKeyboardButton(label).WithCallbackData("lang_set:"+lang))
	}

	btnC := b.T(chatID, "unit_c")
	btnF := b.T(chatID, "unit_f")
	if currentTemp == "c" {
		btnC = "✅ " + btnC
	} else {
		btnF = "✅ " + btnF
	}

	btnMMHG := b.T(chatID, "unit_mmhg")
	btnHPA := b.T(chatID, "unit_hpa")
	if currentPress == "mmhg" {
		btnMMHG = "✅ " + btnMMHG
	} else {
		btnHPA = "✅ " + btnHPA
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
			tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, iconBack)).WithCallbackData("menu_main"),
		),
	)

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_lang_units", iconLang)).
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
	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_prompt_device", iconDevice)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(b.T(chatID, "btn_cancel")).WithCallbackData("menu_list"),
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
			b.sendWithKeyboard(chatID, b.T(chatID, "msg_invalid_device_id", iconAlert), b.mainKeyboard(chatID))
			return
		}
	}

	var text string
	if b.store.Subscribe(chatID, deviceID, b.defaults) {
		text = b.T(chatID, "msg_subscribed", iconSuccess, deviceID)
	} else {
		text = b.T(chatID, "msg_already_sub", iconInfo, deviceID)
	}
	b.sendWithKeyboard(chatID, text, b.subscriptionKeyboard(chatID))
}

func (b *Bot) cmdList(chatID int64) {
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		text := b.T(chatID, "msg_no_subs", iconEmpty, iconSubscribe)
		b.sendWithKeyboard(chatID, text, b.subscriptionKeyboard(chatID))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", iconStatus, id)).WithCallbackData(fmt.Sprintf("status:%s", id)),
		})
	}

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_your_subs", iconList)).
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
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs", iconEmpty, iconSubscribe), b.subscriptionKeyboard(chatID))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s  %s", iconUnsubscribe, id)).WithCallbackData(fmt.Sprintf("unsub:%s", id)),
		})
	}

	// Add Back button
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, iconBack)).WithCallbackData("menu_settings"),
	})

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_unsub", iconDelete)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}
func (b *Bot) cmdStatusMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs", iconEmpty, iconSubscribe), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.sendWithKeyboard(chatID, b.formatDeviceStatus(chatID, devices[0]), b.mainKeyboard(chatID))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", iconStatus, id)).WithCallbackData(fmt.Sprintf("status:%s", id)),
		})
	}

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_device", iconDevice)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) cmdSettings(chatID int64) {
	b.clearLastPrompt(chatID)
	b.sendWithKeyboard(chatID, b.T(chatID, "msg_settings_title", iconSettings), b.settingsKeyboard(chatID))
}

func (b *Bot) cmdChartsMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs", iconEmpty, iconSubscribe), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_charts_menu", iconHistory), b.chartsMenuKeyboard(chatID, devices[0]))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", iconStatus, id)).WithCallbackData(fmt.Sprintf("charts_dev:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, iconBack)).WithCallbackData("menu_main"),
	})

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_device", iconDevice)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) sendChartForDevice(chatID int64, deviceID string, chartType string) {
	hist := b.monitor.GetHistoryByDuration(deviceID, 24*time.Hour)
	if len(hist) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_history_empty", iconHistory, deviceID), b.chartsMenuKeyboard(chatID, deviceID))
		return
	}

	mcfg := b.GetUserSettings(chatID)
	log.Debug().Msgf("Generating %s chart with width=%d, height=%d, fontSize=%.1f", chartType, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	buf, err := generateSingleChart(b, chatID, hist, chartType, mcfg.PM10Green, mcfg.PM25Green, mcfg.PM10Yellow, mcfg.PM25Yellow, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	if err != nil || buf == nil {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_charts", iconAlert, err), b.chartsMenuKeyboard(chatID, deviceID))
		return
	}

	nr := &bytesNamedReader{
		Reader: bytes.NewReader(buf),
		name:   fmt.Sprintf("chart_%s.png", chartType),
	}

	var typeName string
	switch chartType {
	case "pm":
		typeName = b.T(chatID, "chart_pm_title")
	case "temp":
		typeName = b.T(chatID, "msg_temp")
	case "hum":
		typeName = b.T(chatID, "msg_hum")
	case "press":
		typeName = b.T(chatID, "msg_press")
	}

	params := tu.Photo(tu.ID(chatID), tu.File(nr)).
		WithCaption(b.T(chatID, "msg_chart_24h_title", iconHistory, typeName, deviceID)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(b.chartsMenuKeyboard(chatID, deviceID))
	_, err = b.api.SendPhoto(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Msg("failed to send chart")
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_send_ch", iconAlert), b.chartsMenuKeyboard(chatID, deviceID))
	}
}
func (b *Bot) cmdInfo(chatID int64) {
	mcfg := b.GetUserSettings(chatID)
	var sb strings.Builder
	sb.WriteString(b.T(chatID, "msg_info_title", iconInfo))
	sb.WriteString(b.T(chatID, "msg_app_version", config.AppVersion))
	sb.WriteString(b.T(chatID, "msg_bot_version", b.version))
	sb.WriteString(fmt.Sprintf("%s %s: <b>%s</b>\n", iconTemp, b.T(chatID, "msg_temp"), b.unitTempLabel(chatID)))
	sb.WriteString(fmt.Sprintf("%s %s: <b>%s</b>\n", iconPress, b.T(chatID, "msg_press"), b.unitPressLabel(chatID)))
	sb.WriteString(b.T(chatID, "msg_mon_settings", iconSettings))
	sb.WriteString(b.T(chatID, "msg_pm10_info", mcfg.PM10Green, mcfg.PM10Yellow, mcfg.PM10Diff))
	sb.WriteString(b.T(chatID, "msg_pm25_info", mcfg.PM25Green, mcfg.PM25Yellow, mcfg.PM25Diff))

	activeWarnings := make(map[string]bool)
	for _, w := range mcfg.Warnings {
		activeWarnings[w] = true
	}

	type alertInfo struct {
		id   string
		name string
		loud bool
	}

	allAlerts := b.getAllAlerts(chatID)

	sb.WriteString(b.T(chatID, "msg_loud_alerts", iconLoud))
	for _, a := range allAlerts {
		if a.loud {
			icon := iconUnchecked
			if activeWarnings[a.id] {
				icon = iconChecked
			}
			sb.WriteString(fmt.Sprintf("%s %s\n", icon, a.name))
		}
	}

	sb.WriteString("\n" + b.T(chatID, "msg_silent_alerts", iconSilent))
	for _, a := range allAlerts {
		if !a.loud {
			icon := iconUnchecked
			if activeWarnings[a.id] {
				icon = iconChecked
			}
			sb.WriteString(fmt.Sprintf("%s %s\n", icon, a.name))
		}
	}

	b.sendWithKeyboard(chatID, sb.String(), b.settingsKeyboard(chatID))
}

func (b *Bot) getAllAlerts(chatID int64) []struct {
	id   string
	name string
	loud bool
} {
	return []struct {
		id   string
		name string
		loud bool
	}{
		// Sound
		{"val10-yu", b.T(chatID, "alert_val10_yu_short"), true},
		{"val10-ru", b.T(chatID, "alert_val10_ru_short"), true},
		{"val10-yd", b.T(chatID, "alert_val10_yd_short"), true},
		{"val10-gd", b.T(chatID, "alert_val10_gd_short"), true},
		{"val25-yu", b.T(chatID, "alert_val25_yu_short"), true},
		{"val25-ru", b.T(chatID, "alert_val25_ru_short"), true},
		{"val25-yd", b.T(chatID, "alert_val25_yd_short"), true},
		{"val25-gd", b.T(chatID, "alert_val25_gd_short"), true},
		{"vals-yu", b.T(chatID, "alert_vals_yu_short"), true},
		{"vals-ru", b.T(chatID, "alert_vals_ru_short"), true},
		{"vals-yd", b.T(chatID, "alert_vals_yd_short"), true},
		{"vals-gd", b.T(chatID, "alert_vals_gd_short"), true},

		// Silent
		{"diff10-gu", b.T(chatID, "alert_diff10_gu_short"), false},
		{"diff25-gu", b.T(chatID, "alert_diff25_gu_short"), false},
		{"diffs-gu", b.T(chatID, "alert_diffs_gu_short"), false},
		{"diff10-gd", b.T(chatID, "alert_diff10_gd_short"), false},
		{"diff25-gd", b.T(chatID, "alert_diff25_gd_short"), false},
		{"diffs-gd", b.T(chatID, "alert_diffs_gd_short"), false},
		{"diff10-yu", b.T(chatID, "alert_diff10_yu_short"), false},
		{"diff25-yu", b.T(chatID, "alert_diff25_yu_short"), false},
		{"diffs-yu", b.T(chatID, "alert_diffs_yu_short"), false},
		{"diff10-yd", b.T(chatID, "alert_diff10_yd_short"), false},
		{"diff25-yd", b.T(chatID, "alert_diff25_yd_short"), false},
		{"diffs-yd", b.T(chatID, "alert_diffs_yd_short"), false},
		{"diff10-ru", b.T(chatID, "alert_diff10_ru_short"), false},
		{"diff25-ru", b.T(chatID, "alert_diff25_ru_short"), false},
		{"diffs-ru", b.T(chatID, "alert_diffs_ru_short"), false},
		{"diff10-rd", b.T(chatID, "alert_diff10_rd_short"), false},
		{"diff25-rd", b.T(chatID, "alert_diff25_rd_short"), false},
		{"diffs-rd", b.T(chatID, "alert_diffs_rd_short"), false},
	}
}

// cmdThresholdsMenu shows the thresholds submenu.
func (b *Bot) cmdThresholdsMenu(chatID int64) {
	mcfg := b.GetUserSettings(chatID)
	text := b.T(chatID, "msg_thresholds_menu", iconThreshold,
		iconPM10, mcfg.PM10Green, mcfg.PM10Yellow, mcfg.PM10Diff,
		iconPM25, mcfg.PM25Green, mcfg.PM25Yellow, mcfg.PM25Diff)
	b.sendWithKeyboard(chatID, text, b.thresholdsKeyboard(chatID))
}

func (b *Bot) promptThreshold(chatID int64, param, zone string) {
	mcfg := b.GetUserSettings(chatID)
	var titleKey string
	var currentVal float64

	switch param {
	case "PM10":
		switch zone {
		case "green":
			b.setState(chatID, stateAwaitPM10Green)
			titleKey = "msg_threshold_pm10_green"
			currentVal = mcfg.PM10Green
		case "yellow":
			b.setState(chatID, stateAwaitPM10Yellow)
			titleKey = "msg_threshold_pm10_yellow"
			currentVal = mcfg.PM10Yellow
		case "diff":
			b.setState(chatID, stateAwaitDiff10)
			titleKey = "msg_threshold_pm10_diff"
			currentVal = mcfg.PM10Diff
		}
	case "PM2.5":
		switch zone {
		case "green":
			b.setState(chatID, stateAwaitPM25Green)
			titleKey = "msg_threshold_pm25_green"
			currentVal = mcfg.PM25Green
		case "yellow":
			b.setState(chatID, stateAwaitPM25Yellow)
			titleKey = "msg_threshold_pm25_yellow"
			currentVal = mcfg.PM25Yellow
		case "diff":
			b.setState(chatID, stateAwaitDiff25)
			titleKey = "msg_threshold_pm25_diff"
			currentVal = mcfg.PM25Diff
		}
	}

	title := b.T(chatID, titleKey)
	text := b.T(chatID, "msg_threshold_prompt", iconThreshold, title, currentVal)
	b.clearLastPrompt(chatID)
	msg, err := b.api.SendMessage(context.Background(), tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(b.T(chatID, "btn_cancel")).WithCallbackData("menu_thresholds"),
			),
		)))
	if err == nil {
		b.setLastPrompt(chatID, msg.GetMessageID())
	}
}

func (b *Bot) handleThresholdUpdate(chatID int64, msg *telego.Message) {
	b.clearLastPrompt(chatID)
	text := strings.TrimSpace(msg.Text)

	var val float64
	_, err := fmt.Sscanf(text, "%f", &val)
	if err != nil {
		b.setState(chatID, stateIdle)
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_number", iconAlert), b.thresholdsKeyboard(chatID))
		return
	}

	mcfg := b.GetUserSettings(chatID)
	var oldVal float64
	var titleKey string
	state := b.getState(chatID)
	b.setState(chatID, stateIdle)

	switch state {
	case stateAwaitPM10Green:
		oldVal = mcfg.PM10Green
		mcfg.PM10Green = val
		titleKey = "msg_threshold_pm10_green"
	case stateAwaitPM10Yellow:
		if val < mcfg.PM10Green {
			b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_yellow", iconAlert), b.thresholdsKeyboard(chatID))
			return
		}
		oldVal = mcfg.PM10Yellow
		mcfg.PM10Yellow = val
		titleKey = "msg_threshold_pm10_yellow"
	case stateAwaitDiff10:
		oldVal = mcfg.PM10Diff
		mcfg.PM10Diff = val
		titleKey = "msg_threshold_pm10_diff"
	case stateAwaitPM25Green:
		oldVal = mcfg.PM25Green
		mcfg.PM25Green = val
		titleKey = "msg_threshold_pm25_green"
	case stateAwaitPM25Yellow:
		if val < mcfg.PM25Green {
			b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_yellow", iconAlert), b.thresholdsKeyboard(chatID))
			return
		}
		oldVal = mcfg.PM25Yellow
		mcfg.PM25Yellow = val
		titleKey = "msg_threshold_pm25_yellow"
	case stateAwaitDiff25:
		oldVal = mcfg.PM25Diff
		mcfg.PM25Diff = val
		titleKey = "msg_threshold_pm25_diff"
	default:
		return
	}

	b.store.UpdateSettings(chatID, mcfg)
	title := b.T(chatID, titleKey)
	// Send confirmation without keyboard
	b.sendWithKeyboard(chatID, b.T(chatID, "msg_threshold_upd", iconSuccess, title, oldVal, val), nil)
	// Then show the menu
	b.cmdThresholdsMenu(chatID)
}

func (b *Bot) cmdHistoryMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs", iconSubscribe), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.cmdDeviceHistory(chatID, devices[0])
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", iconHistory, id)).WithCallbackData(fmt.Sprintf("history:%s", id)),
		})
	}

	// Add Back button
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, iconBack)).WithCallbackData("menu_main"),
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

	activeWarnings := make(map[string]bool)
	for _, w := range mcfg.Warnings {
		activeWarnings[w] = true
	}

	var rows [][]telego.InlineKeyboardButton
	for _, a := range allAlerts {
		if a.loud != !silent {
			continue
		}

		statusIcon := iconUnchecked
		if activeWarnings[a.id] {
			statusIcon = iconChecked
		}

		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", statusIcon, a.name)).
				WithCallbackData(fmt.Sprintf("toggle:%s:%t", a.id, silent)),
		})
	}

	// Add Back button
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, "btn_mon_settings", iconBack)).WithCallbackData("menu_settings"),
	})

	return tu.InlineKeyboard(rows...)
}

func (b *Bot) cmdSoundMenu(chatID int64, silent bool, editMsgID ...int) {
	var sb strings.Builder
	if silent {
		sb.WriteString(b.T(chatID, "msg_silent_alerts", iconSilent))
	} else {
		sb.WriteString(b.T(chatID, "msg_loud_alerts", iconLoud))
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

// ─── callback handler ─────────────────────────────────────────────────────────

func (b *Bot) handleCallback(cq *telego.CallbackQuery) {
	_ = b.api.AnswerCallbackQuery(context.Background(), tu.CallbackQuery(cq.ID))

	data := cq.Data
	if cq.Message == nil {
		return
	}
	chatID := cq.Message.GetChat().ID

	switch {
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
	case data == "reset_defaults":
		b.cmdResetConfirm(chatID)
	case data == "reset_defaults_yes":
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.cmdResetExecute(chatID)
	case data == "menu_sound":
		b.cmdSoundMenu(chatID, false)
	case data == "menu_silent":
		b.cmdSoundMenu(chatID, true)
	case data == "menu_info":
		b.cmdInfo(chatID)
	case data == "menu_subscribe":
		b.promptDeviceID(chatID)
	case data == "menu_unsubscribe":
		b.cmdUnsubscribeMenu(chatID)
	case strings.HasPrefix(data, "charts_dev:"):
		deviceID := strings.TrimPrefix(data, "charts_dev:")
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_charts_menu", iconHistory), b.chartsMenuKeyboard(chatID, deviceID))

	case strings.HasPrefix(data, "pm_set:"):
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			b.promptThreshold(chatID, parts[1], parts[2])
		}

	case strings.HasPrefix(data, "unsub:"):
		deviceID := strings.TrimPrefix(data, "unsub:")
		if b.store.Unsubscribe(chatID, deviceID) {
			params := tu.EditMessageText(tu.ID(chatID), cq.Message.GetMessageID(),
				b.T(chatID, "msg_unsubscribed", deviceID)).
				WithParseMode(telego.ModeHTML)
			_, _ = b.api.EditMessageText(context.Background(), params)
		}

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
		found := false
		for i, w := range mcfg.Warnings {
			if w == alertID {
				mcfg.Warnings = append(mcfg.Warnings[:i], mcfg.Warnings[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			mcfg.Warnings = append(mcfg.Warnings, alertID)
		}
		b.store.UpdateSettings(chatID, mcfg)
		b.cmdSoundMenu(chatID, silent, cq.Message.GetMessageID())

	case strings.HasPrefix(data, "status:"):
		deviceID := strings.TrimPrefix(data, "status:")
		// Back button returns to device selection list
		b.sendWithKeyboard(chatID, b.formatDeviceStatus(chatID, deviceID), tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(b.T(chatID, btnMainMenu, iconBack)).WithCallbackData("menu_status"),
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
		return b.T(chatID, "status_no_data", iconUnknown, deviceID)
	}
	return b.T(chatID, "msg_status_header", iconStatus) + b.formatMeasurement(chatID, deviceID, m)
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

	mcfg := b.GetUserSettings(chatID)
	log.Debug().Msgf("Generating charts with width=%d, height=%d, fontSize=%.1f", b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	buffers, err := generateCharts(b, chatID, hist, mcfg.PM10Green, mcfg.PM25Green, mcfg.PM10Yellow, mcfg.PM25Yellow, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	if err != nil {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_charts", iconAlert, err), b.mainKeyboard(chatID))
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
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_send_ch", iconAlert), b.mainKeyboard(chatID))
		return
	}

	// Send footer message after the media group
	footer := b.T(chatID, "msg_history_footer", iconHistory, b.sys.ValuesInRam, deviceID)
	b.sendWithKeyboard(chatID, footer, b.mainKeyboard(chatID))
}

func (b *Bot) cmdResetConfirm(chatID int64) {
	b.sendWithKeyboard(chatID, b.T(chatID, "msg_reset_confirm", iconThreshold), b.resetDefaultsKeyboard(chatID))
}

func (b *Bot) cmdResetExecute(chatID int64) {
	mcfg := b.store.GetSettings(chatID, b.defaults)
	mcfg.PM10Green = b.defaults.PM10Green
	mcfg.PM10Yellow = b.defaults.PM10Yellow
	mcfg.PM10Diff = b.defaults.PM10Diff
	mcfg.PM25Green = b.defaults.PM25Green
	mcfg.PM25Yellow = b.defaults.PM25Yellow
	mcfg.PM25Diff = b.defaults.PM25Diff
	mcfg.Warnings = b.defaults.Warnings

	b.store.UpdateSettings(chatID, mcfg)

	text := b.T(chatID, "msg_reset_done", iconSuccess,
		iconPM10, mcfg.PM10Green, mcfg.PM10Yellow, mcfg.PM10Diff,
		iconPM25, mcfg.PM25Green, mcfg.PM25Yellow, mcfg.PM25Diff)

	b.sendWithKeyboard(chatID, text, b.settingsKeyboard(chatID))
}

func (b *Bot) diffTag(pct float64, prev float64, current float64) string {
	if pct > 0 {
		return fmt.Sprintf("\n    %s<b>+%.1f%%</b> <i>(%.2f → %.2f)</i>", iconTrendUp, pct, prev, current)
	}
	if pct < 0 {
		return fmt.Sprintf("\n    %s<b>%.1f%%</b> <i>(%.2f → %.2f)</i>", iconTrendDown, pct, prev, current)
	}
	return fmt.Sprintf(" %s", iconTrendFlat)
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

func (b *Bot) formatMeasurement(chatID int64, deviceID string, m *monitor.Measurement) string {
	var sb strings.Builder

	t := m.Timestamp.Local()
	sb.WriteString(fmt.Sprintf("%s %s %s %s\n\n",
		iconDate, t.Format("02.01.2006"),
		iconTime, t.Format("15:04:05"),
	))

	sb.WriteString(fmt.Sprintf("%s <b>PM10:</b> %.2f мкг/м³", iconPM10, m.PM10))
	if m.PM10Diff != nil {
		sb.WriteString(b.diffTag(*m.PM10Diff, m.PM10Prev, m.PM10))
	}
	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("%s <b>PM2.5:</b> %.2f мкг/м³", iconPM25, m.PM25))
	if m.PM25Diff != nil {
		sb.WriteString(b.diffTag(*m.PM25Diff, m.PM25Prev, m.PM25))
	}
	sb.WriteString("\n")

	if m.Temperature != 0 {
		val := b.convertTemp(m.Temperature, chatID)
		sb.WriteString(fmt.Sprintf("%s <b>%s:</b> %.1f %s\n", iconTemp, b.T(chatID, "msg_temp"), val, b.unitTempLabel(chatID)))
	}
	if m.Humidity != 0 {
		sb.WriteString(fmt.Sprintf("%s <b>%s:</b> %.1f%%\n", iconHum, b.T(chatID, "msg_hum"), m.Humidity))
		if m.Temperature != 0 {
			dp := CalcDewPoint(m.Temperature, m.Humidity)
			dpConverted := b.convertTemp(dp, chatID)
			sb.WriteString(fmt.Sprintf("%s <b>%s:</b> %.1f %s\n", iconDewPoint, b.T(chatID, "msg_dew_point"), dpConverted, b.unitTempLabel(chatID)))
		}
	}
	if m.Pressure != 0 {
		val := b.convertPress(m.Pressure, chatID)
		sb.WriteString(fmt.Sprintf("%s <b>%s:</b> %.1f %s\n", iconPress, b.T(chatID, "msg_press"), val, b.unitPressLabel(chatID)))
	}

	sb.WriteString(fmt.Sprintf("\n%s <b>%s:</b> <code>%s</code>\n", iconDevice, b.T(chatID, "msg_device"), deviceID))

	return sb.String()
}

func (b *Bot) formatWarning(chatID int64, deviceID string, m *monitor.Measurement, messages []string) string {
	return b.formatGeneric(chatID, deviceID, m, iconWarning, b.T(chatID, "msg_alert"), messages)
}

func (b *Bot) formatClear(chatID int64, deviceID string, m *monitor.Measurement, messages []string) string {
	return b.formatGeneric(chatID, deviceID, m, iconSuccess, b.T(chatID, "msg_norma"), messages)
}

func (b *Bot) formatGeneric(chatID int64, deviceID string, m *monitor.Measurement, icon, title string, messages []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s <b>%s</b>\n\n", icon, title))
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("%s %s\n", iconBullet, msg))
	}
	sb.WriteString("\n")
	sb.WriteString(b.formatMeasurement(chatID, deviceID, m))
	return sb.String()
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
