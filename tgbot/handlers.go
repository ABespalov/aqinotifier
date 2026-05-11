package tgbot

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/rs/zerolog/log"

	"github.com/ABespalov/aqinotifier/sensor"
)

func (b *Bot) syncUser(u *telego.User, chatID int64) (changed bool, isNew bool) {
	if u == nil {
		log.Warn().Int64("chat_id", chatID).Msg("tgbot: syncUser failed because User is nil")
		return false, false
	}
	detected := b.detectLang(u.LanguageCode)
	changed, isNew = b.store.SyncLanguage(chatID, u.LanguageCode, detected)
	if changed {
		log.Info().Int64("chat_id", chatID).Str("new", detected).Str("tg_code", u.LanguageCode).Msg("tgbot: language automatically synced from Telegram")
		b.updateCommandsForUser(chatID, detected)
	}
	return changed, isNew
}
func (b *Bot) registerHandlers() {

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		_, isNew := b.syncUser(update.Message.From, chatID)
		b.setState(chatID, stateIdle)
		b.ensureReplyKeyboardRemoved(chatID)
		b.sendHelp(chatID)

		if isNew && len(b.store.Subscriptions(chatID)) == 0 {
			b.promptDeviceID(chatID)
		}

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

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		changed, _ := b.syncUser(update.Message.From, chatID)
		if changed {
			log.Debug().Int64("chat_id", chatID).Msg("tgbot: language changed, refreshing menu")

			b.sendWithKeyboard(chatID, b.T(chatID, "msgHelp"), b.mainKeyboard(chatID))
			return nil
		}
		b.handleMessage(update.Message)
		return nil
	}, th.AnyMessageWithText())

	b.handler.HandleCallbackQuery(func(_ *th.Context, query telego.CallbackQuery) error {
		b.syncUser(&query.From, query.Message.GetChat().ID)
		b.handleCallback(&query)
		return nil
	}, th.AnyCallbackQuery())
}
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
func (b *Bot) handleMessage(msg *telego.Message) {
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	if text == "" {
		return
	}

	lang := b.store.GetLanguage(chatID)
	otherLang := "en"
	if lang == "en" {
		otherLang = "ru"
	}

	match := func(key string, icon string) bool {
		return text == b.T(chatID, key, icon) || text == b.TLang(otherLang, key, icon)
	}

	isCommand := strings.HasPrefix(text, "/")
	isMenuButton := match(btnList, icoList) ||
		match(btnCharts, icoChart) ||
		match(btnStatus, icoStatus) ||
		match(btnSettings, icoSettings) ||
		match(btnHistory, icoHistory) ||
		match(btnSubscribe, icoSubscribe) ||
		match(btnUnsubscribe, icoUnsubscribe) ||
		match(btnMainMenu, icoBack) ||
		match(btnInfo, icoInfo) ||
		strings.Contains(text, "Настройки") || strings.Contains(text, "Статус") ||
		strings.Contains(text, "Графики") || strings.Contains(text, "История") ||
		strings.Contains(text, "Settings") || strings.Contains(text, "Status") ||
		strings.Contains(text, "Charts") || strings.Contains(text, "History")

	if isCommand || isMenuButton {

		b.setState(chatID, stateIdle)

		if isMenuButton {
			params := tu.Message(tu.ID(chatID), icoAqi+" AQI Notifier").
				WithReplyMarkup(tu.ReplyKeyboardRemove())
			_, _ = b.api.SendMessage(context.Background(), params)
		}

		switch {
		case text == "/start" || text == "/help":
			b.sendHelp(chatID)
		case match(btnList, icoList):
			b.cmdList(chatID)
		case match(btnCharts, icoChart):
			b.cmdChartsMenu(chatID)
		case match(btnStatus, icoStatus):
			b.cmdStatusMenu(chatID)
		case match(btnSettings, icoSettings):
			b.cmdSettings(chatID)
		case match(btnHistory, icoHistory):
			b.cmdHistoryMenu(chatID)
		case match(btnSubscribe, icoSubscribe):
			b.promptDeviceID(chatID)
		case match(btnUnsubscribe, icoUnsubscribe):
			b.cmdUnsubscribeMenu(chatID)
		case match(btnMainMenu, icoBack):
			b.sendHelp(chatID)
		}
		return
	}

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
func (b *Bot) promptDeviceID(chatID int64) {
	b.clearLastPrompt(chatID)
	b.setState(chatID, stateAwaitDeviceID)
	params := tu.Message(tu.ID(chatID), b.T(chatID, "msgPromptDevice")).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(b.T(chatID, "btnCancel")).WithCallbackData("menu_list"),
			),
		))
	msg, err := b.api.SendMessage(context.Background(), params)
	if err == nil {
		b.setLastPrompt(chatID, msg.GetMessageID())
	}
}
func (b *Bot) sendChartForDevice(chatID int64, deviceID string, chartType string) {
	hist := b.monitor.GetHistoryByDuration(deviceID, 24*time.Hour)
	if len(hist) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msgHistoryEmpty", map[string]interface {
		}{"device_id": deviceID}), b.chartsMenuKeyboard(chatID, deviceID))
		return
	}

	log.Debug().Msgf("Generating %s chart with width=%d, height=%d, fontSize=%.1f", chartType, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	buf, err := generateSingleChart(b, chatID, hist, chartType, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize, chartSmoothing24h)
	if err != nil || buf == nil {
		b.sendWithKeyboard(chatID, b.T(chatID, "msgErrorCharts", map[string]interface {
		}{"err": err}), b.chartsMenuKeyboard(chatID, deviceID))
		return
	}

	nr := &bytesNamedReader{
		Reader: bytes.NewReader(buf),
		name:   fmt.Sprintf("chart_%s.png", chartType),
	}

	var typeName string
	switch chartType {
	case "pm":
		typeName = b.T(chatID, "txtChartSubjectPm")
	case "temp":
		typeName = b.T(chatID, "txtChartSubjectTemp")
	case "hum":
		typeName = b.T(chatID, "txtChartSubjectHum")
	case "press":
		typeName = b.T(chatID, "txtChartSubjectPress")
	case "aqi":
		typeName = b.T(chatID, "txtChartSubjectAqi")
	}

	mcfg := b.GetUserSettings(chatID)
	deviceName := mcfg.DeviceNames[deviceID]
	params := tu.Photo(tu.ID(chatID), tu.File(nr)).
		WithCaption(b.T(chatID, "msgChart24hTitle", map[string]interface {
		}{"subject": typeName, "deviceId": deviceID, "deviceName": deviceName})).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(b.chartsMenuKeyboard(chatID, deviceID))
	_, err = b.api.SendPhoto(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Msg("failed to send chart")
		b.sendWithKeyboard(chatID, b.T(chatID, "msgErrorSendCh"), b.chartsMenuKeyboard(chatID, deviceID))
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
func (b *Bot) handleThresholdUpdate(chatID int64, msg *telego.Message) {
	b.clearLastPrompt(chatID)
	text := strings.TrimSpace(msg.Text)

	var val float64
	_, err := fmt.Sscanf(text, "%f", &val)
	if err != nil {
		b.setState(chatID, stateIdle)
		b.sendWithKeyboard(chatID, b.T(chatID, "msgErrorNumber"), b.thresholdsKeyboard(chatID))
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
		title = b.T(chatID, "msgThresholdTitleFmt", map[string]interface {
		}{"label": b.T(chatID, "labelThreshold"), "pm": b.T(chatID, "labelPm10"), "zone_str": b.T(chatID, "labelZoneGreenAcc"), "zone_icon": icoGreenSq, "suffix": ""})
	case stateAwaitPM10Yellow:
		if val < mcfg.PM10Green {
			b.sendWithKeyboard(chatID, b.T(chatID, "msgErrorYellow"), b.thresholdsKeyboard(chatID))
			return
		}
		oldVal = mcfg.PM10Yellow
		mcfg.PM10Yellow = val
		title = b.T(chatID, "msgThresholdTitleFmt", map[string]interface {
		}{"label": b.T(chatID, "labelThreshold"), "pm": b.T(chatID, "labelPm10"), "zone_str": b.T(chatID, "labelZoneYellowAcc"), "zone_icon": icoYellowSq, "suffix": ""})
	case stateAwaitDiff10:
		oldVal = mcfg.PM10Diff
		mcfg.PM10Diff = val
		title = b.T(chatID, "msgThresholdDiffTitleFmt", map[string]interface {
		}{"pm": b.T(chatID, "labelPm10"), "label": b.T(chatID, "labelDynamics"), "icon": icoPm10})
	case stateAwaitPM25Green:
		oldVal = mcfg.PM25Green
		mcfg.PM25Green = val
		title = b.T(chatID, "msgThresholdTitleFmt", map[string]interface {
		}{"label": b.T(chatID, "labelThreshold"), "pm": b.T(chatID, "labelPm25"), "zone_str": b.T(chatID, "labelZoneGreenAcc"), "zone_icon": icoGreenSq, "suffix": ""})
	case stateAwaitPM25Yellow:
		if val < mcfg.PM25Green {
			b.sendWithKeyboard(chatID, b.T(chatID, "msgErrorYellow"), b.thresholdsKeyboard(chatID))
			return
		}
		oldVal = mcfg.PM25Yellow
		mcfg.PM25Yellow = val
		title = b.T(chatID, "msgThresholdTitleFmt", map[string]interface {
		}{"label": b.T(chatID, "labelThreshold"), "pm": b.T(chatID, "labelPm25"), "zone_str": b.T(chatID, "labelZoneYellowAcc"), "zone_icon": icoYellowSq, "suffix": ""})
	case stateAwaitDiff25:
		oldVal = mcfg.PM25Diff
		mcfg.PM25Diff = val
		title = b.T(chatID, "msgThresholdDiffTitleFmt", map[string]interface {
		}{"pm": b.T(chatID, "labelPm25"), "label": b.T(chatID, "labelDynamics"), "icon": icoPm25})
	default:
		return
	}

	b.sendWithKeyboard(chatID, b.T(chatID, "msgThresholdUpd", map[string]interface {
	}{"title": title, "old": oldVal, "new": val}), nil)

	b.store.UpdateSettings(chatID, mcfg)
	b.cmdThresholdsMenu(chatID)
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
func (b *Bot) promptThreshold(chatID int64, param, zone string) {
	mcfg := b.GetUserSettings(chatID)
	var currentVal float64

	var pmLabel string
	var zoneLabel string
	var zoneIcon string

	switch param {
	case "PM10":
		pmLabel = b.T(chatID, "labelPm10")
		switch zone {
		case "green":
			b.setState(chatID, stateAwaitPM10Green)
			zoneLabel = b.T(chatID, "labelZoneGreen")
			zoneIcon = icoGreenSq
			currentVal = mcfg.PM10Green
		case "yellow":
			b.setState(chatID, stateAwaitPM10Yellow)
			zoneLabel = b.T(chatID, "labelZoneYellow")
			zoneIcon = icoYellowSq
			currentVal = mcfg.PM10Yellow
		case "diff":
			b.setState(chatID, stateAwaitDiff10)
			zoneLabel = b.T(chatID, "msgThresholdDiffTitle")
			zoneIcon = icoTrendUp
			currentVal = mcfg.PM10Diff
		}
	case "PM2.5":
		pmLabel = b.T(chatID, "labelPm25")
		switch zone {
		case "green":
			b.setState(chatID, stateAwaitPM25Green)
			zoneLabel = b.T(chatID, "labelZoneGreen")
			zoneIcon = icoGreenSq
			currentVal = mcfg.PM25Green
		case "yellow":
			b.setState(chatID, stateAwaitPM25Yellow)
			zoneLabel = b.T(chatID, "labelZoneYellow")
			zoneIcon = icoYellowSq
			currentVal = mcfg.PM25Yellow
		case "diff":
			b.setState(chatID, stateAwaitDiff25)
			zoneLabel = b.T(chatID, "msgThresholdDiffTitle")
			zoneIcon = icoTrendUp
			currentVal = mcfg.PM25Diff
		}
	}

	var title string
	if zone == "diff" {
		title = b.T(chatID, "msgThresholdDiffTitleFmt", map[string]interface {
		}{"pm": pmLabel, "label": zoneLabel, "icon": zoneIcon})
	} else {
		title = b.T(chatID, "msgThresholdTitleFmt", map[string]interface {
		}{"label": b.T(chatID, "labelThreshold"), "pm": pmLabel, "zone_str": zoneLabel, "zone_icon": zoneIcon, "suffix": b.T(chatID, "labelZoneSuffix")})
	}

	text := b.T(chatID, "msgThresholdPrompt", map[string]interface {
	}{"title": title, "curr": currentVal})
	b.clearLastPrompt(chatID)
	msg, err := b.api.SendMessage(context.Background(), tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(b.T(chatID, "btnCancel")).WithCallbackData("menu_thresholds"),
			),
		)))
	if err == nil {
		b.setLastPrompt(chatID, msg.GetMessageID())
	}
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

	text := b.TDevice(chatID, "msgDeviceRenamed", deviceID)
	_, _ = b.api.SendMessage(context.Background(), tu.Message(tu.ID(chatID), text).WithParseMode(telego.ModeHTML))
	b.cmdList(chatID)
}
