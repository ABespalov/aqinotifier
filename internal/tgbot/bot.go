// Package tgbot implements the Telegram bot logic, command handlers, keyboards,
// and state storage.
// This file implements the main Bot lifecycle, authorization, startup, and messaging routines.
package tgbot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"os"
	"path/filepath"

	"github.com/ABespalov/csi18n"
	"github.com/ABespalov/aqinotifier/internal/config"
	"github.com/ABespalov/aqinotifier/internal/monitor"
	"github.com/ABespalov/aqinotifier/internal/sensor"
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
	stateAwaitAQILazyUp
	stateAwaitAQILazyDown
	stateAwaitPM10LazyUp
	stateAwaitPM10LazyDown
	stateAwaitPM25LazyUp
	stateAwaitPM25LazyDown
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
	btnBackDevices         = "btnBackDevices"
	btnAqiBackToThresholds = "btnAqiBackToThresholds"
	btnRename              = "btnRename"
	btnLazySettings        = "btnLazySettings"
	btnCancel              = "btnCancel"
	btnPm25LazyUp          = "btnPm25LazyUp"
	btnPm25LazyDown        = "btnPm25LazyDown"
	btnPm10LazyUp          = "btnPm10LazyUp"
	btnPm10LazyDown        = "btnPm10LazyDown"
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

	ctx      context.Context
	stopFunc context.CancelFunc
	defaults *config.Monitor
	version  string

	renameIDMu sync.Mutex
	renameIDs  map[int64]string

	i18n *csi18n.Translator
}

func NewBot(fullCfg *config.Config, monitorDefaults *config.Monitor, ms *monitor.MonitorService, store SubscriptionStore, translator *csi18n.Translator, version string) (*Bot, error) {
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

	execPath, err := os.Executable()
	baseDir := "."
	if err == nil {
		baseDir = filepath.Dir(execPath)
	}
	resDir := filepath.Join(baseDir, "assets")

	aqiPath := filepath.Join(resDir, "aqi.json")
	if data, err := os.ReadFile(aqiPath); err == nil {
		if err := sensor.LoadStandards(data); err != nil {
			log.Error().Err(err).Msg("tgbot: failed to load AQI standards")
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
		log.Warn().Err(err).Msg("tgbot: failed to read AQI standards file")
	}

	b := &Bot{
		api:       api,
		handler:   handler,
		store:     NewStore(store, cfg.Default.Unit.Temp, cfg.Default.Unit.Press),
		monitor:   ms,
		cfg:       cfg,
		sys:       &fullCfg.System,
		states:    make(map[int64]chatState),
		ctx:       ctx,
		stopFunc:  cancel,
		defaults:  monitorDefaults,
		version:   version,
		renameIDs: make(map[int64]string),
		i18n:      translator,
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

	<-b.ctx.Done()
	log.Info().Msg("tgbot: bot execution context cancelled")
}

func (b *Bot) GetSubscribers(deviceID string) []int64 {
	return b.store.Subscribers(deviceID)
}

func (b *Bot) GetUserSettings(chatID int64) *config.Monitor {
	return b.store.GetSettings(chatID, b.defaults)
}

func (b *Bot) GetDeviceType(deviceID string) string {
	if b.monitor != nil {
		if lm := b.monitor.LastMeasurement(deviceID); lm != nil && lm.DeviceType != "" {
			return lm.DeviceType
		}
	}
	return sensor.DefaultDeviceType
}

func (b *Bot) Notify(chatID int64, m *monitor.Measurement, alerts []monitor.AlertEvent, clears []monitor.AlertEvent, silent bool) {
	if len(alerts) == 0 && len(clears) == 0 {
		return
	}

	allEvents := append([]monitor.AlertEvent{}, alerts...)
	allEvents = append(allEvents, clears...)

	var winnerID string
	var winnerEvent monitor.AlertEvent
	maxPriority := -1
	for _, e := range allEvents {
		p := b.getEventPriority(e.ID)
		if p > maxPriority {
			maxPriority = p
			winnerID = e.ID
			winnerEvent = e
		}
	}

	ctx := b.NewContext(chatID)
	argsMap := ctx.buildMeasurementArgs(m)
	argsMap["winnerID"] = winnerID
	if strings.HasPrefix(winnerID, "aqi_l") {
		argsMap["isAqi"] = true

		var level, prevLevel sensor.AQILevel
		mcfg := b.GetUserSettings(chatID)
		_, level = sensor.CalculateAQI(m.PM25, m.PM10, mcfg.AQI.Standard)
		_, prevLevel = sensor.CalculateAQI(m.PM25Prev, m.PM10Prev, mcfg.AQI.Standard)

		if winnerEvent.HasPrev {
			prevLevel = sensor.AQILevel(winnerEvent.PrevValue)
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
		var sb strings.Builder
		sb.WriteString("evt")
		for _, p := range parts {
			sb.WriteString(strings.Title(p))
		}
		argsMap[sb.String()] = true
	}

	argsMap["isSilent"] = silent
	text := ctx.TDevice("msgAlertNotify", m.DeviceID, argsMap)

	params := tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(ctx.mainKeyboard(m.DeviceID))
	params.DisableNotification = silent

	ctx.clearLastPrompt()
	msg, err := b.api.SendMessage(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Int64("chat_id", chatID).Str("msg", text).Msg("tgbot: failed to send alert")
	} else {
		ctx.setLastPrompt(msg.GetMessageID())
	}
}

func (ctx *RequestContext) sendHelp() {
	ctx.clearLastPrompt()
	ctx.Bot.setState(ctx.ChatID, stateIdle)

	ctx.sendWithKeyboard(ctx.T(msgHelp, map[string]interface{}{
		"botVersion": ctx.Bot.version,
	}), ctx.mainKeyboard())
}

func (b *Bot) registerCommands() {
	langs := b.AvailableLanguages()
	for _, lang := range langs {
		cmds := b.buildCommands(lang)
		
		log.Debug().Str("lang", lang).Interface("cmds", cmds).Msg("tgbot: registering global commands")
		
		err := b.api.SetMyCommands(context.Background(), &telego.SetMyCommandsParams{
			Commands:     cmds,
			LanguageCode: lang,
		})
		if err != nil {
			log.Error().Err(err).Str("lang", lang).Interface("cmds", cmds).Msg("tgbot: failed to set global commands")
		} else {
			log.Debug().Str("lang", lang).Msg("tgbot: successfully set global commands")
		}
	}
}

func (b *Bot) buildCommands(lang string) []telego.BotCommand {
	// Try to get Description keys (*Desc). If they don't exist, we fallback to just text without icons.
	getDesc := func(cmd string) string {
		descKey := cmd + "Desc"
		val := b.TLang(lang, descKey)
		if strings.HasPrefix(val, "!!") {
			// Fallback to the non-Desc key but strip icons (e.g. {icoStart})
			val = b.TLang(lang, cmd)
			if strings.Contains(val, "}") {
				parts := strings.Split(val, "}")
				if len(parts) > 1 {
					val = strings.TrimSpace(parts[len(parts)-1])
				}
			}
		}
		return val
	}

	return []telego.BotCommand{
		{Command: "start", Description: getDesc("cmdStart")},
		{Command: "help", Description: getDesc("cmdHelp")},
		{Command: "status", Description: getDesc("cmdStatus")},
		{Command: "list", Description: getDesc("cmdList")},
		{Command: "subscribe", Description: getDesc("cmdSubscribe")},
		{Command: "unsubscribe", Description: getDesc("cmdUnsubscribe")},
		{Command: "lang", Description: getDesc("cmdLang")},
	}
}

func (b *Bot) updateCommandsForUser(chatID int64, lang string) {
	if lang == "" {
		lang = "en"
	}
	cmds := b.buildCommands(lang)
	
	log.Debug().Int64("chat_id", chatID).Str("lang", lang).Interface("cmds", cmds).Msg("tgbot: registering per-user commands")
	
	// Delete any previously set language-specific commands for this chat to avoid precedence issues
	for _, l := range b.AvailableLanguages() {
		_ = b.api.DeleteMyCommands(context.Background(), &telego.DeleteMyCommandsParams{
			Scope:        &telego.BotCommandScopeChat{Type: "chat", ChatID: tu.ID(chatID)},
			LanguageCode: l,
		})
	}

	err := b.api.SetMyCommands(context.Background(), &telego.SetMyCommandsParams{
		Commands: cmds,
		Scope:    &telego.BotCommandScopeChat{Type: "chat", ChatID: tu.ID(chatID)},
		// Do NOT set LanguageCode here. If we set LanguageCode to the language they chose, 
		// but their Telegram app is in a different language, Telegram will ignore these commands 
		// and fallback to the global commands of their app language. Leaving it empty forces it for this chat.
	})
	if err != nil {
		log.Error().Err(err).Int64("chat_id", chatID).Interface("cmds", cmds).Msg("tgbot: failed to set per-user commands")
	} else {
		log.Debug().Int64("chat_id", chatID).Msg("tgbot: successfully set per-user commands")
	}
}

func (b *Bot) Stop() {
	log.Info().Msg("tgbot: stopping bot...")
	b.stopFunc()
	b.handler.Stop()
}

func (ctx *RequestContext) clearLastPrompt() {
	ids := ctx.Bot.store.GetLastPrompts(ctx.ChatID)
	if len(ids) == 0 {
		return
	}

	ctx.Bot.store.ClearLastPrompts(ctx.ChatID)

	for _, id := range ids {
		_, _ = ctx.Bot.api.EditMessageReplyMarkup(context.Background(), &telego.EditMessageReplyMarkupParams{
			ChatID:      tu.ID(ctx.ChatID),
			MessageID:   id,
			ReplyMarkup: nil,
		})
	}
}

func (ctx *RequestContext) cleanupMessage(cq *telego.CallbackQuery) {
	if cq == nil || cq.Message == nil {
		return
	}
	msgID := cq.Message.GetMessageID()

	// Remove only the keyboard, keep the message text
	_, _ = ctx.Bot.api.EditMessageReplyMarkup(context.Background(), &telego.EditMessageReplyMarkupParams{
		ChatID:      tu.ID(ctx.ChatID),
		MessageID:   msgID,
		ReplyMarkup: nil,
	})

	ctx.Bot.store.RemoveLastPrompt(ctx.ChatID, msgID)
}

func (ctx *RequestContext) deleteMessage(cq *telego.CallbackQuery) {
	if cq == nil || cq.Message == nil {
		return
	}
	msgID := cq.Message.GetMessageID()
	_ = ctx.Bot.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{
		ChatID:    tu.ID(ctx.ChatID),
		MessageID: msgID,
	})
	ctx.Bot.store.RemoveLastPrompt(ctx.ChatID, msgID)
}

func (ctx *RequestContext) handleAQIThresholdCycle(data string, msgID int) {
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

	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)

	var tags []string
	allStds := sensor.GetStandards()
	for tag := range allStds {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	type bpItem struct {
		tag string
		val float64
	}
	var fullList []bpItem
	for _, tag := range tags {
		stdData := allStds[tag]
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
			current = mcfg.PM10.Level1
		} else {
			current = mcfg.PM10.Level2
		}
	} else {
		if levelKey == "level1" {
			current = mcfg.PM25.Level1
		} else {
			current = mcfg.PM25.Level2
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
			if currentTag == "" && strings.EqualFold(item.tag, mcfg.AQI.Standard) {
				currentTag = "✓ "
			}
			if strings.EqualFold(item.tag, mcfg.AQI.Standard) {
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
			mcfg.PM10.Level1 = next
		} else {
			mcfg.PM10.Level2 = next
		}
	} else {
		if levelKey == "level1" {
			mcfg.PM25.Level1 = next
		} else {
			mcfg.PM25.Level2 = next
		}
	}

	ctx.Bot.store.UpdateSettings(ctx.ChatID, mcfg)
	ctx.cmdAQICycleMenu(msgID, map[string]string{
		pmType + ":" + levelKey: nextTag,
	})
}

func (ctx *RequestContext) sendChartForDevice(deviceID, chartType string) {
	hist := ctx.Bot.monitor.GetHistoryByDuration(deviceID, 24*time.Hour)
	if len(hist) == 0 {
		ctx.sendWithKeyboard(ctx.T(msgHistoryError), nil)
		return
	}

	buf, err := generateSingleChart(ctx, hist, chartType, ctx.Bot.cfg.Chart.Width, ctx.Bot.cfg.Chart.Height, ctx.Bot.cfg.Chart.FontSize, chartSmoothing24h)
	if err != nil {
		log.Error().Err(err).Str("device", deviceID).Str("type", chartType).Msg("tgbot: failed to generate chart")
		ctx.sendWithKeyboard(ctx.T(msgHistoryError), nil)
		return
	}

	nr := &bytesNamedReader{Reader: bytes.NewReader(buf), name: "chart.png"}
	params := &telego.SendPhotoParams{
		ChatID:      tu.ID(ctx.ChatID),
		Photo:       tu.File(nr),
		ReplyMarkup: ctx.chartsMenuKeyboard(deviceID),
	}

	ctx.clearLastPrompt()
	m, err := ctx.Bot.api.SendPhoto(context.Background(), params)
	if err == nil {
		ctx.setLastPrompt(m.GetMessageID())
	}
}

func (ctx *RequestContext) formatFormula(f string) string {
	f = strings.TrimSpace(f)
	if strings.HasPrefix(f, "?") {
		parts := strings.Split(f[1:], ":")
		if len(parts) == 3 {
			cond := strings.TrimSpace(parts[0])
			trueVal := strings.TrimSpace(parts[1])
			falseVal := strings.TrimSpace(parts[2])

			// Strip outer parentheses if any
			if strings.HasPrefix(cond, "(") && strings.HasSuffix(cond, ")") {
				cond = cond[1 : len(cond)-1]
			}
			if strings.HasPrefix(trueVal, "(") && strings.HasSuffix(trueVal, ")") {
				trueVal = trueVal[1 : len(trueVal)-1]
			}
			if strings.HasPrefix(falseVal, "(") && strings.HasSuffix(falseVal, ")") {
				falseVal = falseVal[1 : len(falseVal)-1]
			}

			return ctx.T("msgFormulaTernary", map[string]interface{}{
				"cond":     cond,
				"trueVal":  trueVal,
				"falseVal": falseVal,
			})
		}
	}
	return f
}

func (ctx *RequestContext) buildDeviceSettingsText(deviceID string) string {
	devType := ctx.Bot.GetDeviceType(deviceID)
	stdDisplay := sensor.DeviceStandard(devType).DisplayName()

	var correctionsList string
	var hasCorrections bool
	if ctx.Bot.defaults != nil && ctx.Bot.defaults.Corrections != nil {
		if corrs, ok := ctx.Bot.defaults.Corrections[devType]; ok && len(corrs) > 0 {
			var keys []string
			for k := range corrs {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			var lines []string
			for _, k := range keys {
				formatted := ctx.formatFormula(corrs[k])
				fieldName := ctx.T("labelField_" + k)
				if strings.HasPrefix(fieldName, "!!") {
					fieldName = k
				}
				line := ctx.T("msgDeviceSettingsCorrItem", map[string]interface{}{
					"key":     fieldName,
					"formula": formatted,
				})
				lines = append(lines, line)
			}
			correctionsList = strings.Join(lines, "\n")
			hasCorrections = len(lines) > 0
		}
	}

	args := map[string]interface{}{
		"type":            stdDisplay,
		"hasCorrections":  hasCorrections,
		"correctionsList": correctionsList,
	}

	return ctx.TDevice("msgDeviceSettings", deviceID, args)
}

func (b *Bot) TLang(lang, key string, args ...interface{}) string {
	return b.i18n.T(lang, key, args...)
}

func (b *Bot) Resolve(s string, args ...interface{}) string {
	return b.i18n.Resolve(s, args...)
}

func (b *Bot) AvailableLanguages() []string {
	return b.i18n.AvailableLanguages()
}

func (b *Bot) detectLang(langCode string) string {
	for _, l := range b.AvailableLanguages() {
		if strings.HasPrefix(langCode, l) {
			return l
		}
	}
	return "en"
}

