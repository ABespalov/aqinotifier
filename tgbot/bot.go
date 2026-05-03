package tgbot

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"html"
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

const BotVersion = "0.4.3a"

// chatState tracks what the bot is waiting for from a specific chat.
type chatState int

const (
	stateIdle               chatState = iota
	stateAwaitDeviceID                // waiting for user to type a device ID to subscribe
	stateAwaitPM10Threshold           // waiting for user to type PM10 threshold value
	stateAwaitPM25Threshold           // waiting for user to type PM2.5 threshold value
	stateAwaitDiffTime                // waiting for user to type diff time interval
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
	btnInterval       = "btn_interval"
	btnThresholds     = "btn_thresholds"
	btnSoundProfiles  = "btn_sound_profiles"
	btnSilentProfiles = "btn_silent_profiles"
	btnPM10Threshold  = "btn_pm10_threshold"
	btnPM25Threshold  = "btn_pm25_threshold"
	btnCharts         = "btn_charts"
	btnChartPM        = "btn_chart_pm"
	btnChartTemp      = "btn_chart_temp"
	btnChartHum       = "btn_chart_hum"
	btnChartPress     = "btn_chart_press"
)

// Icons used throughout the bot UI.
const (
	iconBot         = "🌬️" // Bot header
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
	iconInfo        = "ℹ️" // Information
	iconEmpty       = "📭"  // Empty
	iconDelete      = "🗑"  // Deletion
	iconDevice      = "📡"  // Device / ID
	iconUnknown     = "❓"  // No data
	iconTrendUp     = "🔺"  // Trend Up (growth)
	iconTrendDown   = "▼"  // Trend Down (decrease)
	iconTrendFlat   = "—"  // Flat (no change)
	iconClock       = "⏱"  // Intervals
	iconBullet      = "•"  // List marker
	iconLoud        = "🔊"  // With sound
	iconSilent      = "🔕"  // Without sound
	iconThreshold   = "⚖️" // Thresholds
	iconChecked     = "✅"  // Enabled
	iconUnchecked   = "❌"  // Disabled
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

	stateMu sync.Mutex
	states  map[int64]chatState

	stopFunc context.CancelFunc
	defaults *config.Monitor
	version  string
}

// NewBot creates and starts the Telegram bot using Telego library.
func NewBot(cfg *config.TgBot, monitorDefaults *config.Monitor, ms *monitor.MonitorService) (*Bot, error) {
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
		states:   make(map[int64]chatState),
		stopFunc: cancel,
		defaults: monitorDefaults,
		version:  BotVersion,
	}

	b.registerHandlers()
	b.registerCommands()

	self, err := api.GetMe(context.Background())
	if err != nil {
		return nil, fmt.Errorf("tgbot: failed to get bot info: %w", err)
	}
	log.Info().Str("username", self.Username).Msg("tgbot: bot authorized (telego)")
	return b, nil
}

// registerCommands sets the localized command descriptions for the bot.
func (b *Bot) registerCommands() {
	// Default (English) commands
	cmdsEN := []telego.BotCommand{
		{Command: "start", Description: b.TLang("en", "cmd_start_desc")},
		{Command: "list", Description: b.TLang("en", "cmd_list_desc")},
		{Command: "status", Description: b.TLang("en", "cmd_status_desc")},
		{Command: "help", Description: b.TLang("en", "cmd_help_desc")},
		{Command: "lang", Description: b.TLang("en", "cmd_lang_desc")},
	}
	_ = b.api.SetMyCommands(context.Background(), &telego.SetMyCommandsParams{Commands: cmdsEN})

	// Russian commands
	cmdsRU := []telego.BotCommand{
		{Command: "start", Description: b.TLang("ru", "cmd_start_desc")},
		{Command: "list", Description: b.TLang("ru", "cmd_list_desc")},
		{Command: "status", Description: b.TLang("ru", "cmd_status_desc")},
		{Command: "help", Description: b.TLang("ru", "cmd_help_desc")},
		{Command: "lang", Description: b.TLang("ru", "cmd_lang_desc")},
	}
	_ = b.api.SetMyCommands(context.Background(), &telego.SetMyCommandsParams{
		Commands:     cmdsRU,
		LanguageCode: "ru",
	})
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
		b.cmdSubscribeDevice(chatID, parts[1])
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
			b.sendWithKeyboard(chatID, b.T(chatID, "msg_help", iconBot, iconList, iconSettings), b.mainKeyboard(chatID))
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
func (b *Bot) mainKeyboard(chatID int64) *telego.ReplyKeyboardMarkup {
	return &telego.ReplyKeyboardMarkup{
		Keyboard: [][]telego.KeyboardButton{
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnSettings, iconSettings)),
				tu.KeyboardButton(b.T(chatID, btnStatus, iconStatus)),
			),
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnCharts, iconHistory)),
				tu.KeyboardButton(b.T(chatID, btnHistory, iconHistory)),
			),
		},
		ResizeKeyboard: true,
		IsPersistent:   true,
	}
}

// settingsKeyboard returns the keyboard for the settings menu.
func (b *Bot) settingsKeyboard(chatID int64) *telego.ReplyKeyboardMarkup {
	return &telego.ReplyKeyboardMarkup{
		Keyboard: [][]telego.KeyboardButton{
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnList, iconList)),
				tu.KeyboardButton(b.T(chatID, btnInterval, iconClock)),
			),
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnThresholds, iconThreshold)),
				tu.KeyboardButton(b.T(chatID, btnSoundProfiles, iconLoud)),
			),
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnSilentProfiles, iconSilent)),
				tu.KeyboardButton(b.T(chatID, btnMainMenu, iconBack)),
			),
		},
		ResizeKeyboard: true,
		IsPersistent:   true,
	}
}

// thresholdsKeyboard returns the keyboard for the thresholds submenu.

func (b *Bot) chartsMenuKeyboard(chatID int64) *telego.ReplyKeyboardMarkup {
	return &telego.ReplyKeyboardMarkup{
		Keyboard: [][]telego.KeyboardButton{
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnChartPM, iconPM10)),
				tu.KeyboardButton(b.T(chatID, btnChartTemp, iconTemp)),
			),
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnChartHum, iconHum)),
				tu.KeyboardButton(b.T(chatID, btnChartPress, iconPress)),
			),
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnMainMenu, iconBack)),
			),
		},
		ResizeKeyboard: true,
		IsPersistent:   true,
	}
}
func (b *Bot) thresholdsKeyboard(chatID int64) *telego.ReplyKeyboardMarkup {
	return &telego.ReplyKeyboardMarkup{
		Keyboard: [][]telego.KeyboardButton{
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnPM10Threshold, iconPM10)),
				tu.KeyboardButton(b.T(chatID, btnPM25Threshold, iconPM25)),
			),
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnMainMenu, iconBack)),
			),
		},
		ResizeKeyboard: true,
		IsPersistent:   true,
	}
}

// subscriptionKeyboard returns the keyboard for the subscription management menu.
func (b *Bot) subscriptionKeyboard(chatID int64) *telego.ReplyKeyboardMarkup {
	return &telego.ReplyKeyboardMarkup{
		Keyboard: [][]telego.KeyboardButton{
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnSubscribe, iconSubscribe)),
				tu.KeyboardButton(b.T(chatID, btnUnsubscribe, iconUnsubscribe)),
			),
			tu.KeyboardRow(
				tu.KeyboardButton(b.T(chatID, btnMainMenu, iconBack)),
			),
		},
		ResizeKeyboard: true,
		IsPersistent:   true,
	}
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
		WithParseMode(telego.ModeHTML)
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

	state := b.getState(chatID)
	switch state {
	case stateAwaitDeviceID:
		b.setState(chatID, stateIdle)
		b.cmdSubscribeDevice(chatID, text)
		return
	case stateAwaitPM10Threshold:
		b.setState(chatID, stateIdle)
		b.handleThresholdUpdate(chatID, "PM10", text)
		return
	case stateAwaitPM25Threshold:
		b.setState(chatID, stateIdle)
		b.handleThresholdUpdate(chatID, "PM2.5", text)
		return
	case stateAwaitDiffTime:
		b.setState(chatID, stateIdle)
		b.handleIntervalUpdate(chatID, text)
		return
	}

	lang := b.store.GetLanguage(chatID)
	log.Debug().Int64("chat_id", chatID).Str("text", text).Str("lang", lang).Interface("state", state).Msg("tgbot: received message")

	if text == "/start" || text == "/help" {
		log.Debug().Msg("tgbot: handling /start or /help")
		if text == "/start" && len(b.store.Subscriptions(chatID)) == 0 {
			b.sendHelp(chatID)
			b.promptDeviceID(chatID)
		} else {
			b.sendHelp(chatID)
		}
		return
	}

	// Robust matching: if it doesn't match current lang, try the OTHER lang
	// This helps during language transitions
	otherLang := "en"
	if b.store.GetLanguage(chatID) == "en" {
		otherLang = "ru"
	}

	match := func(key string, icon string) bool {
		return text == b.T(chatID, key, icon) || text == b.TLang(otherLang, key, icon)
	}

	switch {
	case match(btnList, iconList):
		b.cmdList(chatID)
	case match(btnCharts, iconHistory):
		b.cmdChartsMenu(chatID)
	case match(btnChartPM, iconPM10):
		b.cmdDeviceChart(chatID, "pm")
	case match(btnChartTemp, iconTemp):
		b.cmdDeviceChart(chatID, "temp")
	case match(btnChartHum, iconHum):
		b.cmdDeviceChart(chatID, "hum")
	case match(btnChartPress, iconPress):
		b.cmdDeviceChart(chatID, "press")
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
		b.setState(chatID, stateIdle)
		b.sendWithKeyboard(chatID, b.T(chatID, btnMainMenu, iconBack), b.mainKeyboard(chatID))
	case match(btnInterval, iconClock):
		b.cmdIntervalPrompt(chatID)
	case match(btnThresholds, iconThreshold):
		b.cmdThresholdsMenu(chatID)
	case match(btnSoundProfiles, iconLoud):
		b.cmdSoundMenu(chatID, false)
	case match(btnSilentProfiles, iconSilent):
		b.cmdSoundMenu(chatID, true)
	case match(btnPM10Threshold, iconPM10):
		b.promptThreshold(chatID, "PM10")
	case match(btnPM25Threshold, iconPM25):
		b.promptThreshold(chatID, "PM2.5")
	case match(btnInfo, iconInfo):
		b.cmdInfo(chatID)
	}
}

// ─── commands ─────────────────────────────────────────────────────────────────

func (b *Bot) cmdLangMenu(chatID int64) {
	currentLang := b.store.GetLanguage(chatID)
	currentTemp := b.store.GetUnitTemp(chatID)
	currentPress := b.store.GetUnitPress(chatID)

	btnRU := b.T(chatID, "lang_ru")
	btnEN := b.T(chatID, "lang_en")
	if currentLang == "ru" {
		btnRU = "✅ " + btnRU
	} else {
		btnEN = "✅ " + btnEN
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
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnRU).WithCallbackData("lang_set:ru"),
			tu.InlineKeyboardButton(btnEN).WithCallbackData("lang_set:en"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnC).WithCallbackData("unit_set:temp:c"),
			tu.InlineKeyboardButton(btnF).WithCallbackData("unit_set:temp:f"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnMMHG).WithCallbackData("unit_set:press:mmhg"),
			tu.InlineKeyboardButton(btnHPA).WithCallbackData("unit_set:press:hpa"),
		),
	)

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_lang_units")).
		WithReplyMarkup(inlineKeyboard)
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) updateCommandsForUser(chatID int64, lang string) {
	if lang == "" {
		lang = "en"
	}
	cmds := []telego.BotCommand{
		{Command: "start", Description: b.TLang(lang, "cmd_start_desc")},
		{Command: "list", Description: b.TLang(lang, "cmd_list_desc")},
		{Command: "status", Description: b.TLang(lang, "cmd_status_desc")},
		{Command: "help", Description: b.TLang(lang, "cmd_help_desc")},
		{Command: "lang", Description: b.TLang(lang, "cmd_lang_desc")},
	}
	log.Debug().Int64("chat_id", chatID).Str("lang", lang).Msg("tgbot: updating commands for user scope")
	_ = b.api.SetMyCommands(context.Background(), &telego.SetMyCommandsParams{
		Commands: cmds,
		Scope:    &telego.BotCommandScopeChat{Type: "chat", ChatID: tu.ID(chatID)},
	})
}

func (b *Bot) sendHelp(chatID int64) {
	log.Debug().Int64("chat_id", chatID).Msg("tgbot: sending help")
	text := b.T(chatID, "msg_help", iconBot, iconList, iconSettings)

	params := tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(b.mainKeyboard(chatID))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) promptDeviceID(chatID int64) {
	b.setState(chatID, stateAwaitDeviceID)
	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_prompt_device", iconDevice)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(&telego.ForceReply{ForceReply: true})
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) cmdSubscribeDevice(chatID int64, deviceID string) {
	deviceID = strings.TrimSpace(deviceID)
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

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_unsub", iconDelete)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) cmdStatusMenu(chatID int64) {
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
	b.sendWithKeyboard(chatID, b.T(chatID, "msg_settings_title", iconSettings), b.settingsKeyboard(chatID))
}

func (b *Bot) cmdChartsMenu(chatID int64) {
	b.sendWithKeyboard(chatID, b.T(chatID, "msg_charts_menu", iconHistory), b.chartsMenuKeyboard(chatID))
}

func (b *Bot) cmdDeviceChart(chatID int64, chartType string) {
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs", iconEmpty, iconSubscribe), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.sendChartForDevice(chatID, devices[0], chartType)
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", iconStatus, id)).WithCallbackData(fmt.Sprintf("chart:%s:%s", chartType, id)),
		})
	}

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_device", iconDevice)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) sendChartForDevice(chatID int64, deviceID string, chartType string) {
	hist := b.monitor.GetHistoryByDuration(deviceID, 24*time.Hour)
	if len(hist) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_history_empty", iconHistory, deviceID), b.chartsMenuKeyboard(chatID))
		return
	}

	mcfg := b.GetUserSettings(chatID)
	log.Debug().Msgf("Generating %s chart with width=%d, height=%d, fontSize=%.1f", chartType, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	buf, err := generateSingleChart(b, chatID, hist, chartType, mcfg.PM10Value, mcfg.PM25Value, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	if err != nil || buf == nil {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_charts", iconAlert, err), b.chartsMenuKeyboard(chatID))
		return
	}

	nr := &bytesNamedReader{
		Reader: bytes.NewReader(buf),
		name:   fmt.Sprintf("chart_%s.png", chartType),
	}

	params := tu.Photo(tu.ID(chatID), tu.File(nr)).
		WithCaption(b.T(chatID, "msg_history_title", iconHistory, deviceID)).
		WithParseMode(telego.ModeHTML)
	_, err = b.api.SendPhoto(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Msg("failed to send chart")
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_send_ch", iconAlert), b.chartsMenuKeyboard(chatID))
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
	sb.WriteString(b.T(chatID, "msg_interval_info", mcfg.DiffTime))
	sb.WriteString(b.T(chatID, "msg_pm10_info", mcfg.PM10Value, mcfg.PM10Diff))
	sb.WriteString(b.T(chatID, "msg_pm25_info", mcfg.PM25Value, mcfg.PM25Diff))

	activeWarnings := make(map[string]bool)
	for _, w := range mcfg.Warnings {
		activeWarnings[w] = true
	}

	type alertInfo struct {
		id   string
		name string
		loud bool
	}

	allAlerts := []alertInfo{
		{"val10", b.T(chatID, "alert_val10"), true},
		{"val25", b.T(chatID, "alert_val25"), true},
		{"vals", b.T(chatID, "alert_vals"), true},
		{"diff10_neg_over", b.T(chatID, "alert_diff10_neg_over"), true},
		{"diff25_neg_over", b.T(chatID, "alert_diff25_neg_over"), true},
		{"diffs_neg_over", b.T(chatID, "alert_diffs_neg_over"), true},

		{"diff10", b.T(chatID, "alert_diff10"), false},
		{"diff25", b.T(chatID, "alert_diff25"), false},
		{"diffs", b.T(chatID, "alert_diffs"), false},
		{"diff10_neg", b.T(chatID, "alert_diff10_neg"), false},
		{"diff25_neg", b.T(chatID, "alert_diff25_neg"), false},
		{"diffs_neg", b.T(chatID, "alert_diffs_neg"), false},
		{"diff10_over", b.T(chatID, "alert_diff10_over"), false},
		{"diff25_over", b.T(chatID, "alert_diff25_over"), false},
		{"diffs_over", b.T(chatID, "alert_diffs_over"), false},
	}

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

// cmdThresholdsMenu shows the thresholds submenu.
func (b *Bot) cmdThresholdsMenu(chatID int64) {
	mcfg := b.GetUserSettings(chatID)
	text := b.T(chatID, "msg_thresholds_menu", iconThreshold, iconPM10, mcfg.PM10Value, iconPM25, mcfg.PM25Value)
	b.sendWithKeyboard(chatID, text, b.thresholdsKeyboard(chatID))
}

func (b *Bot) cmdIntervalPrompt(chatID int64) {
	mcfg := b.GetUserSettings(chatID)
	b.setState(chatID, stateAwaitDiffTime)
	text := b.T(chatID, "msg_interval_prompt", iconClock, mcfg.DiffTime)
	b.sendWithKeyboard(chatID, text, &telego.ForceReply{ForceReply: true})
}

func (b *Bot) promptThreshold(chatID int64, param string) {
	if param == "PM10" {
		b.setState(chatID, stateAwaitPM10Threshold)
	} else {
		b.setState(chatID, stateAwaitPM25Threshold)
	}
	text := b.T(chatID, "msg_threshold_prompt", iconThreshold, param)
	b.sendWithKeyboard(chatID, text, &telego.ForceReply{ForceReply: true})
}

func (b *Bot) handleThresholdUpdate(chatID int64, param, text string) {
	var val float64
	_, err := fmt.Sscanf(text, "%f", &val)
	if err != nil {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_number", iconAlert), b.thresholdsKeyboard(chatID))
		return
	}

	mcfg := b.GetUserSettings(chatID)
	if param == "PM10" {
		mcfg.PM10Value = val
	} else {
		mcfg.PM25Value = val
	}
	b.store.UpdateSettings(chatID, mcfg)

	b.sendWithKeyboard(chatID, b.T(chatID, "msg_threshold_upd", iconSuccess, param, val), b.thresholdsKeyboard(chatID))
	b.cmdThresholdsMenu(chatID)
}

func (b *Bot) handleIntervalUpdate(chatID int64, text string) {
	var val int
	_, err := fmt.Sscanf(text, "%d", &val)
	if err != nil || val <= 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_positive", iconAlert), b.settingsKeyboard(chatID))
		return
	}

	mcfg := b.GetUserSettings(chatID)
	mcfg.DiffTime = val
	b.store.UpdateSettings(chatID, mcfg)

	b.sendWithKeyboard(chatID, b.T(chatID, "msg_interval_upd", iconSuccess, val), b.settingsKeyboard(chatID))
	b.cmdSettings(chatID)
}

func (b *Bot) cmdHistoryMenu(chatID int64) {
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

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_history")).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) cmdSoundMenu(chatID int64, silent bool, editMsgID ...int) {
	mcfg := b.GetUserSettings(chatID)

	var sb strings.Builder
	if silent {
		sb.WriteString(b.T(chatID, "msg_silent_alerts", iconSilent))
	} else {
		sb.WriteString(b.T(chatID, "msg_loud_alerts"))
	}
	sb.WriteString("\n" + b.T(chatID, "msg_sound_settings"))

	type alertInfo struct {
		id   string
		name string
		loud bool
	}

	allAlerts := []alertInfo{
		{"val10", b.T(chatID, "alert_val10"), true},
		{"val25", b.T(chatID, "alert_val25"), true},
		{"vals", b.T(chatID, "alert_vals"), true},
		{"diff10_neg_over", b.T(chatID, "alert_diff10_neg_over"), true},
		{"diff25_neg_over", b.T(chatID, "alert_diff25_neg_over"), true},
		{"diffs_neg_over", b.T(chatID, "alert_diffs_neg_over"), true},

		{"diff10", b.T(chatID, "alert_diff10"), false},
		{"diff25", b.T(chatID, "alert_diff25"), false},
		{"diffs", b.T(chatID, "alert_diffs"), false},
		{"diff10_neg", b.T(chatID, "alert_diff10_neg"), false},
		{"diff25_neg", b.T(chatID, "alert_diff25_neg"), false},
		{"diffs_neg", b.T(chatID, "alert_diffs_neg"), false},
		{"diff10_over", b.T(chatID, "alert_diff10_over"), false},
		{"diff25_over", b.T(chatID, "alert_diff25_over"), false},
		{"diffs_over", b.T(chatID, "alert_diffs_over"), false},
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

		statusIcon := iconUnchecked
		if activeWarnings[a.id] {
			statusIcon = iconChecked
		}

		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", statusIcon, a.name)).
				WithCallbackData(fmt.Sprintf("toggle:%s:%t", a.id, silent)),
		})
	}

	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(chatID), editMsgID[0], sb.String()).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(tu.InlineKeyboard(rows...))
		_, _ = b.api.EditMessageText(context.Background(), params)
	} else {
		params := tu.Message(tu.ID(chatID), sb.String()).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(tu.InlineKeyboard(rows...))
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

		// Update commands for this user
		b.updateCommandsForUser(chatID, lang)

		// Delete the language selection message
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{
			ChatID:    tu.ID(chatID),
			MessageID: cq.Message.GetMessageID(),
		})

		// Send help message and refresh keyboard in the new language
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_help", iconBot, iconList, iconSettings), b.mainKeyboard(chatID))

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

		// Refresh menu
		b.cmdSoundMenu(chatID, silent, cq.Message.GetMessageID())

	case strings.HasPrefix(data, "status:"):
		deviceID := strings.TrimPrefix(data, "status:")
		b.sendWithKeyboard(chatID, b.formatDeviceStatus(chatID, deviceID), b.mainKeyboard(chatID))

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
	return b.formatMeasurement(chatID, deviceID, m)
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
	buffers, err := generateCharts(b, chatID, hist, mcfg.PM10Value, mcfg.PM25Value, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
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

		// Add caption to the first image
		if i == 0 {
			photo.Caption = b.T(chatID, "msg_history_title", iconHistory, deviceID)
			photo.ParseMode = telego.ModeHTML
		}
		media = append(media, photo)
	}

	params := tu.MediaGroup(tu.ID(chatID), media...)
	_, err = b.api.SendMediaGroup(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Msg("failed to send history charts")
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_send_ch", iconAlert), b.mainKeyboard(chatID))
	}
}

func (b *Bot) diffTag(chatID int64, pct float64, prev float64) string {
	if pct > 0 {
		return fmt.Sprintf("\n    %s<b>+%.1f%%</b> <i>(%s %.2f)</i>", iconTrendUp, pct, b.T(chatID, "msg_was"), prev)
	}
	if pct < 0 {
		return fmt.Sprintf("\n    %s<b>%.1f%%</b> <i>(%s %.2f)</i>", iconTrendDown, pct, b.T(chatID, "msg_was"), prev)
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
		sb.WriteString(b.diffTag(chatID, *m.PM10Diff, m.PM10Prev))
	}
	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("%s <b>PM2.5:</b> %.2f мкг/м³", iconPM25, m.PM25))
	if m.PM25Diff != nil {
		sb.WriteString(b.diffTag(chatID, *m.PM25Diff, m.PM25Prev))
	}
	sb.WriteString("\n")

	if m.Temperature != 0 {
		val := b.convertTemp(m.Temperature, chatID)
		sb.WriteString(fmt.Sprintf("%s <b>%s:</b> %.1f %s\n", iconTemp, b.T(chatID, "msg_temp"), val, b.unitTempLabel(chatID)))
	}
	if m.Humidity != 0 {
		sb.WriteString(fmt.Sprintf("%s <b>%s:</b> %.1f%%\n", iconHum, b.T(chatID, "msg_hum"), m.Humidity))
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
		sb.WriteString(fmt.Sprintf("%s %s\n", iconBullet, html.EscapeString(msg)))
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
