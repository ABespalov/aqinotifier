package tgbot

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"math"
	"sort"
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
	btnPM10Level1          = "btnPm10L1"
	btnPM25Level1          = "btnPm25L1"
	btnPM10Level2          = "btnPm10L2"
	btnPM25Level2          = "btnPm25L2"
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
	kIcoAqi          = "icoAqi"
	kIcoStatus       = "icoStatus"
	kIcoSettings     = "icoSettings"
	kIcoHistory      = "icoHistory"
	kIcoChart        = "icoChart"
	kIcoSubscribe    = "icoSubscribe"
	kIcoUnsubscribe  = "icoUnsubscribe"
	kIcoBack         = "icoBack"
	kIcoBackSettings = "icoBackSettings"
	kIcoReset        = "icoReset"
	kIcoInfo         = "icoInfo"
	kIcoSuccess      = "icoSuccess"
	kIcoError        = "icoError"
	kIcoWarning      = "icoWarning"
	kIcoAlert        = "icoAlert"
	kIcoLoud         = "icoLoud"
	kIcoSilent       = "icoSilent"
	kIcoEmpty        = "icoEmpty"
	kIcoUnknown      = "icoUnknown"
	kIcoDate         = "icoDate"
	kIcoTime         = "icoTime"
	kIcoTemp         = "icoTemp"
	kIcoHum          = "icoHum"
	kIcoPress        = "icoPress"
	kIcoDewPoint     = "icoDewPoint"
	kIcoPm10         = "icoPm10"
	kIcoPm25         = "icoPm25"
	kIcoTrendUp      = "icoTrendUp"
	kIcoTrendDown    = "icoTrendDown"
	kIcoTrendFlat    = "icoTrendFlat"
	kIcoPollution    = "icoPollution"
	kIcoChecked      = "icoChecked"
	kIcoUnchecked    = "icoUnchecked"
	kIcoBullet       = "icoBullet"
	kIcoThreshold    = "icoThreshold"
	kIcoSetByAQI     = "icoSetByAQI"
	kIcoWrite        = "icoWrite"
	kIcoPlant        = "icoPlant"
	kIcoDevice       = "icoDevice"
	kIcoList         = "icoList"
	kIcoLang         = "icoLang"
	kIcoDelete       = "icoDelete"
	kIcoFlagEU       = "icoFlagEU"
	kIcoFlagUS       = "icoFlagUS"
	kIcoDynamics     = "icoDynamics"
	kIcoLevels       = "icoLevels"

	kIcoPmLevel1 = "icoPmLevel1"
	kIcoPmLevel2 = "icoPmLevel2"
	kIcoPmLevel3 = "icoPmLevel3"

	kIcoAqiUSLevel1 = "icoAqiUSLevel1"
	kIcoAqiUSLevel2 = "icoAqiUSLevel2"
	kIcoAqiUSLevel3 = "icoAqiUSLevel3"
	kIcoAqiUSLevel4 = "icoAqiUSLevel4"
	kIcoAqiUSLevel5 = "icoAqiUSLevel5"
	kIcoAqiUSLevel6 = "icoAqiUSLevel6"
	kIcoAqiUSLevel7 = "icoAqiUSLevel7"

	kIcoAqiEULevel1 = "icoAqiEULevel1"
	kIcoAqiEULevel2 = "icoAqiEULevel2"
	kIcoAqiEULevel3 = "icoAqiEULevel3"
	kIcoAqiEULevel4 = "icoAqiEULevel4"
	kIcoAqiEULevel5 = "icoAqiEULevel5"
	kIcoAqiEULevel6 = "icoAqiEULevel6"
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

		var level, prevLevel sensor.AQILevel
		mcfg := b.GetUserSettings(chatID)
		if mcfg.AQIStandard == "US" {
			_, level = sensor.CalculateUS_AQI(m.PM25, m.PM10)
			_, prevLevel = sensor.CalculateUS_AQI(m.PM25Prev, m.PM10Prev)
		} else {
			_, level = sensor.CalculateEU_AQI(m.PM25, m.PM10)
			_, prevLevel = sensor.CalculateEU_AQI(m.PM25Prev, m.PM10Prev)
		}

		argsMap["isRise"] = level > prevLevel
		argsMap["isFall"] = level < prevLevel
		argsMap["isReturn"] = level == sensor.LevelGood
	} else {
		argsMap["isRise"] = strings.Contains(winnerID, "u") || strings.Contains(winnerID, "rise")
		argsMap["isFall"] = strings.Contains(winnerID, "d") || strings.Contains(winnerID, "fall")
		argsMap["isReturn"] = strings.Contains(winnerID, "l1d") || strings.Contains(winnerID, "l2d") || strings.Contains(winnerID, "clean") || strings.Contains(winnerID, "return")
		argsMap["isSharp"] = strings.Contains(winnerID, "rise") || strings.Contains(winnerID, "fall")

		argsMap["isBoth"] = strings.Contains(winnerID, "vals") || strings.Contains(winnerID, "diffs")
		argsMap["isPm10"] = strings.Contains(winnerID, "10") || argsMap["isBoth"].(bool)
		argsMap["isPm25"] = strings.Contains(winnerID, "25") || argsMap["isBoth"].(bool)
		argsMap["isAqi"] = strings.Contains(winnerID, "aqi") || strings.Contains(winnerID, "diffs")

		isVal := strings.HasPrefix(winnerID, "val")
		isDiff := strings.HasPrefix(winnerID, "diff")
		argsMap["isPm10Val"] = argsMap["isPm10"].(bool) && isVal
		argsMap["isPm10Diff"] = argsMap["isPm10"].(bool) && isDiff
		argsMap["isPm25Val"] = argsMap["isPm25"].(bool) && isVal
		argsMap["isPm25Diff"] = argsMap["isPm25"].(bool) && isDiff

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

	argsMap["isSilent"] = silent
	text := b.TDevice(chatID, "msgAlertNotify", m.DeviceID, argsMap)

	params := tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(b.mainKeyboard(chatID, m.DeviceID))
	params.DisableNotification = silent
	_, err := b.api.SendMessage(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Int64("chat_id", chatID).Str("msg", text).Msg("tgbot: failed to send alert")
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

	// Remove only the keyboard, keep the message text
	_, _ = b.api.EditMessageReplyMarkup(context.Background(), &telego.EditMessageReplyMarkupParams{
		ChatID:      tu.ID(chatID),
		MessageID:   msgID,
		ReplyMarkup: nil,
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
	if len(parts) < 3 {
		return
	}
	pmType := parts[1]
	levelKey := parts[2]
	currentTag := ""
	if len(parts) >= 4 {
		currentTag = parts[3]
	}

	mcfg := b.GetUserSettings(chatID)

	var tags []string
	for tag := range sensor.Standards {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	type bpItem struct {
		tag string
		val float64
	}
	var fullList []bpItem
	for _, tag := range tags {
		stdData := sensor.Standards[tag]
		var list []float64
		if pmType == "PM10" {
			list = stdData.Breakpoints10
		} else {
			list = stdData.Breakpoints25
		}
		for _, v := range list {
			fullList = append(fullList, bpItem{tag: tag, val: v})
		}
	}

	if len(fullList) == 0 {
		return
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

	const eps = 0.0001
	idx := -1
	for i, item := range fullList {
		if math.Abs(item.val-current) < eps {
			if idx == -1 {
				idx = i
			}
			if currentTag != "" && strings.EqualFold(item.tag, currentTag) {
				idx = i
				break
			}
			// Fallback: if no currentTag, try to match user's default standard
			if currentTag == "" && strings.EqualFold(item.tag, mcfg.AQIStandard) {
				idx = i
				break
			}
		}
	}

	nextIdx := (idx + 1) % len(fullList)
	next := fullList[nextIdx].val
	nextTag := fullList[nextIdx].tag

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
	b.cmdAQICycleMenu(chatID, msgID, map[string]string{
		pmType + ":" + levelKey: nextTag,
	})
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
