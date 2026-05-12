package tgbot

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/rs/zerolog/log"
)

type bytesNamedReader struct {
	io.Reader
	name string
}

func (r *bytesNamedReader) Name() string {
	return r.name
}

type chatState int

const (
	stateIdle chatState = iota
	stateAwaitPM10Level1
	stateAwaitPM10Level2
	stateAwaitPM25Level1
	stateAwaitPM25Level2
	stateAwaitDiff10
	stateAwaitDiff25
	stateAwaitDeviceName
	stateAwaitDeviceID
)

const (
	btnList                = "btnList"
	btnStatus              = "btnStatus"
	btnSettings            = "btnSettings"
	btnHistory             = "btnHistory"
	btnSubscribe           = "btnSubscribe"
	btnUnsubscribe         = "btnUnsubscribe"
	btnMainMenu            = "btnMainMenu"
	btnThresholds          = "btnThresholds"
	btnPM10Level1          = "btnPm10Green"
	btnPM25Level1          = "btnPm25Green"
	btnPM10Level2          = "btnPm10Yellow"
	btnPM25Level2          = "btnPm25Yellow"
	btnPM10Diff            = "btnPm10Diff"
	btnPM25Diff            = "btnPm25Diff"
	btnCharts              = "btnCharts"
	btnSetByAQI            = "btnSetPmByAqi"
	btnChartPM             = "btnChartPm"
	btnChartTemp           = "btnChartTemp"
	btnChartHum            = "btnChartHum"
	btnChartPress          = "btnChartPress"
	btnResetDefaults       = "btnResetDefaults"
	btnAQISettings         = "btnAqiSettings"
	btnChartAQI            = "btnChartAqi"
	btnSoundProfiles       = "btnSoundProfiles"
	btnSilentProfiles      = "btnSilentProfiles"
	btnYes                 = "btnYes"
	btnNo                  = "btnNo"
	btnLang                = "btnLang"
	btnBackSettings        = "btnBackSettings"
	btnAqiBackToThresholds = "btnAqiBackToThresholds"
	btnRename              = "btnRename"
	btnCancel              = "btnCancel"
)

const (
	cmdStatus           = "menu_status"
	cmdSettings         = "menu_settings"
	cmdHistory          = "menu_history"
	cmdCharts           = "menu_charts"
	cmdList             = "menu_list"
	cmdThresholdsMenu   = "menu_thresholds"
	cmdAQISettings      = "menu_aqi"
	cmdResetSettings    = "reset_defaults"
	cmdResetDefaultsYes = "reset_defaults_yes"
	cmdHelp             = "menu_main"
	cmdSoundProfiles    = "menu_sound"
	cmdSilentProfiles   = "menu_silent"
	cmdLang             = "menu_lang"
	cmdSubscribe        = "menu_subscribe"
	cmdUnsubscribe      = "menu_unsubscribe"
	cmdAqiCycleMenu     = "menu_aqi_cycle"
	cmdCancelThreshold  = "cancel_threshold"
	cmdCancelSub        = "cancel_sub"

	cmdPM10Level1 = "pm_set:PM10:level1"
	cmdPM10Level2 = "pm_set:PM10:level2"
	cmdPM25Level1 = "pm_set:PM2.5:level1"
	cmdPM25Level2 = "pm_set:PM2.5:level2"
	cmdPM10Diff   = "pm_set:PM10:diff"
	cmdPM25Diff   = "pm_set:PM2.5:diff"
)

const (
	msgHelp            = "msgHelp"
	msgSettingsTitle   = "msgSettingsTitle"
	msgChartsMenu      = "msgChartsMenu"
	msgAqiSettings     = "msgAqiSettings"
	msgThresholdsMenu  = "msgThresholdsMenu"
	msgAqiCycleMenu    = "msgAqiCycleMenu"
	msgSilentAlerts    = "msgSilentAlerts"
	msgLoudAlerts      = "msgLoudAlerts"
	msgSoundSettings   = "msgSoundSettings"
	msgRenamePrompt    = "msgRenamePrompt"
	msgHistoryFooter   = "msgHistoryFooter"
	msgResetExecution  = "msgResetExecution"
	msgYourSubs        = "msgYourSubs"
	msgSelectUnsub     = "msgSelectUnsub"
	msgSelectDevice    = "msgSelectDevice"
	msgSelectHistory   = "msgSelectHistory"
	msgNoSubs          = "msgNoSubs"
	msgInvalidDeviceId = "msgInvalidDeviceId"
	msgSubscribed      = "msgSubscribed"
	msgAlreadySub      = "msgAlreadySub"
	msgSelectLang      = "msgSelectLang"
	msgHistoryEmpty    = "msgHistoryEmpty"
	msgHistoryError    = "msgHistoryError"
	msgRenameCancel    = "msgRenameCancel"
	msgUnsubConfirm    = "msgUnsubConfirm"
	msgUnsubscribed    = "msgUnsubscribed"
)

const (
	kIcoFlagEU    = "icoFlagEU"
	kIcoFlagUS    = "icoFlagUS"
	kIcoTrendUp   = "icoTrendUp"
	kIcoTrendDown = "icoTrendDown"
	kIcoPmLevel1  = "icoPmLevel1"
	kIcoPmLevel2  = "icoPmLevel2"
	kIcoPmLevel3  = "icoPmLevel3"
	kIcoSuccess   = "icoSuccess"
	kIcoError     = "icoError"
	kIcoStatus    = "icoStatus"
	kIcoDelete    = "icoDelete"
	kIcoHistory   = "icoHistory"
	kIcoWrite     = "icoWrite"
	kIcoChecked   = "icoChecked"
	kIcoDevice    = "icoDevice"
	kIcoSettings  = "icoSettings"
	kIcoUnchecked = "icoUnchecked"
	kIcoLoud      = "icoLoud"
	kIcoSilent    = "icoSilent"
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
	lastPrompts   map[int64][]int

	renameIDMu sync.Mutex
	renameIDs  map[int64]string
}

func NewBot(fullCfg *config.Config, monitorDefaults *config.Monitor, ms *monitor.MonitorService, version string) (*Bot, error) {
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
		version:     version,
		lastPrompts: make(map[int64][]int),
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

func (b *Bot) Run() {
	b.handler.Start()
	log.Info().Msg("tgbot: bot is running")

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

	argsMap := b.buildMeasurementArgs(chatID, m)
	argsMap["winnerID"] = winnerID
	if strings.HasPrefix(winnerID, "aqi_l") {
		argsMap["isAqi"] = true
		argsMap["isRise"] = strings.Contains(winnerID, "rise")
		argsMap["isFall"] = strings.Contains(winnerID, "fall")
		argsMap["isReturn"] = strings.Contains(winnerID, "return")
		argsMap["isRed"] = strings.Contains(winnerID, "l4") || strings.Contains(winnerID, "l5") || strings.Contains(winnerID, "l6")
		argsMap["isYellow"] = strings.Contains(winnerID, "l2") || strings.Contains(winnerID, "l3")
	} else {
		argsMap["isRise"] = strings.Contains(winnerID, "u") || strings.Contains(winnerID, "rise")
		argsMap["isFall"] = strings.Contains(winnerID, "d") || strings.Contains(winnerID, "fall")
		argsMap["isReturn"] = strings.Contains(winnerID, "l1d") || strings.Contains(winnerID, "l2d") || strings.Contains(winnerID, "clean") || strings.Contains(winnerID, "return")
		argsMap["isSharp"] = strings.Contains(winnerID, "rise") || strings.Contains(winnerID, "fall")

		argsMap["isBoth"] = strings.Contains(winnerID, "vals")
		argsMap["isPm10"] = strings.Contains(winnerID, "val10") || strings.Contains(winnerID, "pm10") || argsMap["isBoth"].(bool)
		argsMap["isPm25"] = strings.Contains(winnerID, "val25") || strings.Contains(winnerID, "pm25") || argsMap["isBoth"].(bool)
		argsMap["isAqi"] = strings.Contains(winnerID, "aqi")

		argsMap["isRed"] = strings.Contains(winnerID, "l3")
		argsMap["isYellow"] = strings.Contains(winnerID, "l2")
		argsMap["isGreen"] = strings.Contains(winnerID, "l1")
	}

	argsMap["isAlert"] = len(alerts) > 0
	argsMap["isNorma"] = len(clears) > 0 && len(alerts) == 0

	for _, e := range allEvents {
		parts := strings.FieldsFunc(e.ID, func(r rune) bool { return r == '-' || r == '_' })
		key := "evt"
		for _, p := range parts {
			key += strings.Title(p)
		}
		argsMap[key] = true
	}

	text := b.TDevice(chatID, "msgAlertNotify", m.DeviceID, argsMap)

	params := tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(b.mainKeyboard(chatID, m.DeviceID))
	params.DisableNotification = silent
	_, err := b.api.SendMessage(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Int64("chat_id", chatID).Msg("tgbot: failed to send alert")
	}
}

func (b *Bot) sendHelp(chatID int64) {
	b.clearLastPrompt(chatID)
	b.setState(chatID, stateIdle)

	b.sendWithKeyboard(chatID, b.T(chatID, msgHelp, map[string]interface{}{
		"botVersion": b.version,
	}), b.mainKeyboard(chatID))
}

func (b *Bot) registerCommands() {
	langs := AvailableLanguages()
	for _, lang := range langs {
		cmds := b.buildCommands(lang)
		err := b.api.SetMyCommands(context.Background(), &telego.SetMyCommandsParams{
			Commands:     cmds,
			LanguageCode: lang,
		})
		if err != nil {
			log.Error().Err(err).Str("lang", lang).Msg("tgbot: failed to set global commands")
		}
	}
}

func (b *Bot) buildCommands(lang string) []telego.BotCommand {
	return []telego.BotCommand{
		{Command: "start", Description: b.TLang(lang, "cmdStart")},
		{Command: "help", Description: b.TLang(lang, "cmdHelp")},
		{Command: "status", Description: b.TLang(lang, "cmdStatus")},
		{Command: "list", Description: b.TLang(lang, "cmdList")},
		{Command: "subscribe", Description: b.TLang(lang, "cmdSubscribe")},
		{Command: "unsubscribe", Description: b.TLang(lang, "cmdUnsubscribe")},
		{Command: "lang", Description: b.TLang(lang, "cmdLang")},
	}
}

func (b *Bot) updateCommandsForUser(chatID int64, lang string) {
	if lang == "" {
		lang = "en"
	}
	cmds := b.buildCommands(lang)
	err := b.api.SetMyCommands(context.Background(), &telego.SetMyCommandsParams{
		Commands:     cmds,
		Scope:        &telego.BotCommandScopeChat{Type: "chat", ChatID: tu.ID(chatID)},
		LanguageCode: lang,
	})
	if err != nil {
		log.Error().Err(err).Int64("chat_id", chatID).Msg("tgbot: failed to set per-user commands")
	}
}

func (b *Bot) Stop() {
	log.Info().Msg("tgbot: stopping bot...")
	b.stopFunc()
	b.handler.Stop()
}

func (b *Bot) clearLastPrompt(chatID int64) {
	b.lastPromptsMu.Lock()
	ids, ok := b.lastPrompts[chatID]
	if ok {
		delete(b.lastPrompts, chatID)
	}
	b.lastPromptsMu.Unlock()

	if ok {
		for _, id := range ids {
			_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{
				ChatID:    tu.ID(chatID),
				MessageID: id,
			})
		}
	}
}

func (b *Bot) cleanupMessage(chatID int64, cq *telego.CallbackQuery) {
	if cq == nil || cq.Message == nil {
		return
	}
	msgID := cq.Message.GetMessageID()

	// Surgical delete: only the message that triggered the callback
	_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{
		ChatID:    tu.ID(chatID),
		MessageID: msgID,
	})

	// Remove only this message from lastPrompts
	b.lastPromptsMu.Lock()
	if ids, ok := b.lastPrompts[chatID]; ok {
		newIDs := make([]int, 0, len(ids))
		for _, id := range ids {
			if id != msgID {
				newIDs = append(newIDs, id)
			}
		}
		if len(newIDs) == 0 {
			delete(b.lastPrompts, chatID)
		} else {
			b.lastPrompts[chatID] = newIDs
		}
	}
	b.lastPromptsMu.Unlock()
}

func (b *Bot) handleAQIThresholdCycle(chatID int64, data string, msgID int) {
	parts := strings.Split(data, ":")
	if len(parts) != 4 {
		return
	}
	pmType := parts[2]
	levelKey := parts[3]

	mcfg := b.GetUserSettings(chatID)

	std := mcfg.AQIStandard
	var bp []float64
	if pmType == "PM10" {
		if std == "US" {
			bp = sensor.BreakpointsUS10
		} else {
			bp = sensor.BreakpointsEU10
		}
	} else {
		if std == "US" {
			bp = sensor.BreakpointsUS25
		} else {
			bp = sensor.BreakpointsEU25
		}
	}

	var current float64
	if pmType == "PM10" {
		if levelKey == "level1" {
			current = mcfg.PM10L1
		} else {
			current = mcfg.PM10L2
		}
	} else {
		if levelKey == "level1" {
			current = mcfg.PM25L1
		} else {
			current = mcfg.PM25L2
		}
	}

	next := bp[0]
	for i, v := range bp {
		if v == current {
			if i+1 < len(bp) {
				next = bp[i+1]
			} else {
				next = bp[0]
			}
			break
		}
	}

	if pmType == "PM10" {
		if levelKey == "level1" {
			mcfg.PM10L1 = next
		} else {
			mcfg.PM10L2 = next
		}
	} else {
		if levelKey == "level1" {
			mcfg.PM25L1 = next
		} else {
			mcfg.PM25L2 = next
		}
	}

	b.store.UpdateSettings(chatID, mcfg)
	b.cmdAQICycleMenu(chatID, msgID)
}

func (b *Bot) sendChartForDevice(chatID int64, deviceID, chartType string) {
	hist := b.monitor.GetHistoryByDuration(deviceID, 24*time.Hour)
	if len(hist) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, msgHistoryError), nil)
		return
	}

	buf, err := generateSingleChart(b, chatID, hist, chartType, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize, chartSmoothing24h)
	if err != nil {
		log.Error().Err(err).Str("device", deviceID).Str("type", chartType).Msg("tgbot: failed to generate chart")
		b.sendWithKeyboard(chatID, b.T(chatID, msgHistoryError), nil)
		return
	}

	nr := &bytesNamedReader{Reader: bytes.NewReader(buf), name: "chart.png"}
	params := &telego.SendPhotoParams{
		ChatID:      tu.ID(chatID),
		Photo:       tu.File(nr),
		ReplyMarkup: b.chartsMenuKeyboard(chatID, deviceID),
	}
	m, err := b.api.SendPhoto(context.Background(), params)
	if err == nil {
		b.setLastPrompt(chatID, m.GetMessageID())
	}
}

func (b *Bot) SetDB(db *sql.DB) {
	if b.store != nil {
		b.store.SetDB(db)
	}
}
