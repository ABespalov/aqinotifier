// Package tgbot implements the Telegram bot logic, command handlers, keyboards,
// and state storage.
// This file implements various Telegram bot command handlers (such as subscribe,
// status, list, settings) and user settings prompt handlers.
package tgbot

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ABespalov/aqinotifier/internal/config"
	"github.com/ABespalov/aqinotifier/internal/sensor"
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/rs/zerolog/log"
)

func (ctx *RequestContext) cmdSubscribeDevice(msg *telego.Message) {
	ctx.clearLastPrompt()
	input := strings.TrimSpace(msg.Text)

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
		ctx.promptDeviceID()
		return
	}

	for _, c := range deviceID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			ctx.sendWithKeyboard(ctx.T(msgInvalidDeviceId), ctx.mainKeyboard())
			return
		}
	}

	var text string
	if ctx.Bot.store.Subscribe(ctx.ChatID, deviceID, ctx.Bot.defaults) {
		text = ctx.TDevice(msgSubscribed, deviceID)
	} else {
		text = ctx.TDevice(msgAlreadySub, deviceID)
	}
	ctx.sendWithKeyboard(text, nil)
	ctx.cmdList()
}

func (ctx *RequestContext) cmdSettings() {
	ctx.clearLastPrompt()
	ctx.sendWithKeyboard(ctx.T(msgSettingsTitle), ctx.settingsKeyboard())
}

func (ctx *RequestContext) cmdStatusMenu() {
	ctx.clearLastPrompt()
	devices := ctx.Bot.store.Subscriptions(ctx.ChatID)
	if len(devices) == 0 {
		ctx.sendWithKeyboard(ctx.T(msgNoSubs), ctx.subscriptionKeyboard())
		return
	}
	if len(devices) == 1 {
		ctx.sendWithKeyboard(ctx.formatDeviceStatus(devices[0]), ctx.mainKeyboard(devices[0]))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", ctx.Bot.TLang("en", kIcoStatus), id)).WithCallbackData(fmt.Sprintf("status:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(ctx.T(btnMainMenu)).WithCallbackData(cmdHelp),
	})

	ctx.sendWithKeyboard(ctx.T(msgSelectDevice), tu.InlineKeyboard(rows...))
}

func (ctx *RequestContext) cmdList() {
	log.Debug().Int64("chat_id", ctx.ChatID).Msg("tgbot: cmdList called")
	ctx.clearLastPrompt()
	devices := ctx.Bot.store.Subscriptions(ctx.ChatID)

	var text string
	if len(devices) == 0 {
		text = ctx.T(msgNoSubs)
	} else {
		text = ctx.T(msgYourSubs)
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", ctx.Bot.TLang("en", kIcoDevice), ctx.formatDeviceIDPlain(id))).WithCallbackData(fmt.Sprintf("dev_settings:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(ctx.T(btnSubscribe)).WithCallbackData(cmdSubscribe),
		tu.InlineKeyboardButton(ctx.T(btnBackSettings)).WithCallbackData(cmdSettings),
	})

	ctx.sendWithKeyboard(text, tu.InlineKeyboard(rows...))
}

func (ctx *RequestContext) cmdUnsubscribeMenu() {
	ctx.clearLastPrompt()
	devices := ctx.Bot.store.Subscriptions(ctx.ChatID)
	if len(devices) == 0 {
		ctx.sendWithKeyboard(ctx.T(msgNoSubs), ctx.subscriptionKeyboard())
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", ctx.Bot.TLang("en", kIcoDelete), ctx.formatDeviceIDPlain(id))).WithCallbackData(fmt.Sprintf("unsub:%s", id)),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(ctx.T(btnBackSettings)).WithCallbackData(cmdSettings),
	})

	ctx.sendWithKeyboard(ctx.T(msgSelectUnsub), tu.InlineKeyboard(rows...))
}

func (ctx *RequestContext) cmdChartsMenu() {
	ctx.clearLastPrompt()
	devices := ctx.Bot.store.Subscriptions(ctx.ChatID)
	if len(devices) == 0 {
		ctx.sendWithKeyboard(ctx.T(msgNoSubs), ctx.subscriptionKeyboard())
		return
	}
	if len(devices) == 1 {
		ctx.sendWithKeyboard(ctx.T(msgChartsMenu), ctx.chartsMenuKeyboard(devices[0]))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", ctx.Bot.TLang("en", kIcoTrendUp), id)).WithCallbackData(fmt.Sprintf("charts_dev:%s", id)),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(ctx.T(btnBackSettings)).WithCallbackData(cmdSettings),
	})

	ctx.sendWithKeyboard(ctx.T(msgSelectDevice), tu.InlineKeyboard(rows...))
}

func (ctx *RequestContext) cmdLangMenu(editMsgID ...int) {
	if len(editMsgID) == 0 {
		ctx.clearLastPrompt()
	}

	var langRow []telego.InlineKeyboardButton
	current := ctx.Bot.store.GetLanguage(ctx.ChatID)
	unitT := ctx.Bot.store.GetUnitTemp(ctx.ChatID)
	unitP := ctx.Bot.store.GetUnitPress(ctx.ChatID)

	for _, l := range ctx.Bot.AvailableLanguages() {
		name := ctx.Bot.TLang(l, "lang"+strings.Title(l))
		
		label := ""
		if l == current {
			label = fmt.Sprintf("%s %s", ctx.Bot.TLang("en", "icoChecked"), name)
		} else {
			label = name
		}
		langRow = append(langRow, tu.InlineKeyboardButton(label).WithCallbackData("lang_set:"+l))
	}

	kb := tu.InlineKeyboard(
		langRow,
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T("txtUnit"+strings.Title(unitT))).WithCallbackData("unit_set:temp:toggle"),
			tu.InlineKeyboardButton(ctx.T("txtUnit"+strings.Title(unitP))).WithCallbackData("unit_set:press:toggle"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnBackSettings)).WithCallbackData(cmdSettings),
		),
	)

	text := ctx.T(msgSelectLang)
	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(ctx.ChatID), editMsgID[0], text).
			WithReplyMarkup(kb)
		_, _ = ctx.Bot.api.EditMessageText(context.Background(), params)
	} else {
		ctx.sendWithKeyboard(text, kb)
	}
}

func (ctx *RequestContext) cmdAqiMenu(editMsgID ...int) {
	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	stdTag := strings.ToUpper(mcfg.AQI.Standard)
	std := sensor.GetStandard(stdTag)

	var details string
	if std != nil {
		stdName := std.NameFull
		stdLocKey := "standard" + strings.Title(strings.ToLower(stdTag))
		if loc := ctx.T(stdLocKey); !strings.HasPrefix(loc, "!!") {
			stdName = loc
		}

		title := ctx.T("txtAqiStandardTitle", map[string]interface{}{
			"std":  stdName,
			"flag": ctx.Bot.Resolve(std.Flag),
		})

		var zones []string
		for _, z := range std.Zones {
			var aqiVal float64
			if z.Level < len(std.IndexPoints) {
				aqiVal = std.IndexPoints[z.Level]
			}

			name := z.Name
			key := fmt.Sprintf("aqiNameL%d%s", z.Level, strings.Title(strings.ToLower(stdTag)))
			if loc := ctx.T(key); !strings.HasPrefix(loc, "!!") {
				name = loc
			}

			zones = append(zones, ctx.T("txtAqiStandardZone", map[string]interface{}{
				"ico":  ctx.Bot.Resolve(z.Icon),
				"name": name,
				"bp":   aqiVal,
			}))
		}
		details = title + "\n" + strings.Join(zones, "\n")
	}

	text := ctx.T(msgAqiSettings) + "\n\n" + details
	kb := ctx.aqiSettingsKeyboard()

	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(ctx.ChatID), editMsgID[0], text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = ctx.Bot.api.EditMessageText(context.Background(), params)
	} else {
		ctx.sendWithKeyboard(text, kb)
	}
}

func (ctx *RequestContext) cmdThresholdsMenu() {
	ctx.clearLastPrompt()
	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	text := ctx.T(msgThresholdsMenu, map[string]interface{}{
		"l1_25": mcfg.PM25.Level1, "l2_25": mcfg.PM25.Level2, "dyn25": mcfg.PM25.Diff,
		"l1_10": mcfg.PM10.Level1, "l2_10": mcfg.PM10.Level2, "dyn10": mcfg.PM10.Diff,
		"labelPm25": ctx.T("labelPm25"), "labelPm10": ctx.T("labelPm10"),
		"labelL1": ctx.T("labelL1"), "labelL2": ctx.T("labelL2"),
		"labelDynamics": ctx.T("labelDynamics"),
	})

	ctx.sendWithKeyboard(text, ctx.thresholdsKeyboard())
}

func (ctx *RequestContext) cmdAQICycleMenu(editMsgID int, suggestedTags ...map[string]string) {
	sTags := make(map[string]string)
	if len(suggestedTags) > 0 {
		sTags = suggestedTags[0]
	}
	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	std := strings.ToUpper(mcfg.AQI.Standard)
	if sensor.GetStandard(std) == nil {
		return
	}

	text := ctx.T(msgAqiCycleMenu, map[string]interface{}{
		"l1_25": mcfg.PM25.Level1, "l2_25": mcfg.PM25.Level2,
		"l1_10": mcfg.PM10.Level1, "l2_10": mcfg.PM10.Level2,
	})

	getIcon := func(pmType string, val float64) (string, string, string) {
		activeTag := strings.ToUpper(mcfg.AQI.Standard)

		// Check active standard first
		if stdData := sensor.GetStandard(activeTag); stdData != nil {
			var bp []float64
			if pmType == "PM10" {
				bp = stdData.Breakpoints10
			} else {
				bp = stdData.Breakpoints25
			}
			for _, v := range bp {
				if math.Abs(v-val) < 0.0001 {
					_, level := sensor.CalculateValueAQI(val, pmType, activeTag)
					return activeTag, ctx.Bot.Resolve(stdData.Flag), ctx.Bot.getAQIIcon(level, activeTag)
				}
			}
		}

		// Get other tags in sorted order
		var otherTags []string
		allStds := sensor.GetStandards()
		for tag := range allStds {
			if tag != activeTag {
				otherTags = append(otherTags, tag)
			}
		}
		sort.Strings(otherTags)

		// Check others
		for _, tag := range otherTags {
			stdData := allStds[tag]
			var bp []float64
			if pmType == "PM10" {
				bp = stdData.Breakpoints10
			} else {
				bp = stdData.Breakpoints25
			}
			for _, v := range bp {
				if math.Abs(v-val) < 0.0001 {
					_, level := sensor.CalculateValueAQI(val, pmType, tag)
					return tag, ctx.Bot.Resolve(stdData.Flag), ctx.Bot.getAQIIcon(level, tag)
				}
			}
		}
		return "", ctx.Bot.TLang("en", kIcoWrite), ""
	}

	btn := func(pmType, levelKey, zoneIcon string, val float64) telego.InlineKeyboardButton {
		key := pmType + ":" + levelKey
		var tag, flag, levelIcon string
		if sTag, ok := sTags[key]; ok {
			tag = sTag
			if stdData := sensor.GetStandard(tag); stdData != nil {
				flag = stdData.Flag
				_, level := sensor.CalculateValueAQI(val, pmType, tag)
				levelIcon = ctx.Bot.getAQIIcon(level, tag)
			}
		}

		if tag == "" {
			tag, flag, levelIcon = getIcon(pmType, val)
		}

		label := fmt.Sprintf("%s%s ⇐ %s%s %.1f", pmType, zoneIcon, flag, levelIcon, val)
		if levelIcon == "" {
			label = fmt.Sprintf("%s%s ⇐ %s %.1f", pmType, zoneIcon, flag, val)
		}
		return tu.InlineKeyboardButton(label).WithCallbackData(fmt.Sprintf("aqi_cycle:%s:%s:%s", pmType, levelKey, tag))
	}

	kb := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			btn("PM25", "level1", ctx.Bot.TLang("en", kIcoPmLevel1), mcfg.PM25.Level1),
			btn("PM10", "level1", ctx.Bot.TLang("en", kIcoPmLevel1), mcfg.PM10.Level1),
		),
		tu.InlineKeyboardRow(
			btn("PM25", "level2", ctx.Bot.TLang("en", kIcoPmLevel2), mcfg.PM25.Level2),
			btn("PM10", "level2", ctx.Bot.TLang("en", kIcoPmLevel2), mcfg.PM10.Level2),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnAqiBackToThresholds)).WithCallbackData(cmdThresholdsMenu),
		),
	)

	if editMsgID > 0 {
		params := tu.EditMessageText(tu.ID(ctx.ChatID), editMsgID, text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = ctx.Bot.api.EditMessageText(context.Background(), params)
	} else {
		ctx.sendWithKeyboard(text, kb)
	}
}

func (ctx *RequestContext) cmdHistoryMenu() {
	log.Debug().Int64("chat_id", ctx.ChatID).Msg("tgbot: cmdHistoryMenu called")
	ctx.clearLastPrompt()
	devices := ctx.Bot.store.Subscriptions(ctx.ChatID)
	if len(devices) == 0 {
		ctx.sendWithKeyboard(ctx.T(msgNoSubs), ctx.subscriptionKeyboard())
		return
	}
	if len(devices) == 1 {
		ctx.cmdDeviceHistory(devices[0])
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", ctx.Bot.TLang("en", kIcoHistory), id)).WithCallbackData(fmt.Sprintf("history:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(ctx.T(btnBackSettings)).WithCallbackData(cmdSettings),
	})

	ctx.sendWithKeyboard(ctx.T(msgSelectHistory), tu.InlineKeyboard(rows...))
}

func (ctx *RequestContext) cmdSoundMenu(silent bool, editMsgID ...int) {
	var templateKey string
	if silent {
		templateKey = msgSilentAlerts
	} else {
		templateKey = msgLoudAlerts
	}
	text := ctx.T(templateKey) + ctx.T(msgSoundSettings)

	kb := ctx.notificationSettingsKeyboard(silent)

	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(ctx.ChatID), editMsgID[0], text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = ctx.Bot.api.EditMessageText(context.Background(), params)
	} else {
		ctx.clearLastPrompt()
		ctx.sendWithKeyboard(text, kb)
	}
}

func (ctx *RequestContext) cmdRename(deviceID string) {
	ctx.Bot.setState(ctx.ChatID, stateAwaitDeviceName)
	ctx.Bot.renameIDMu.Lock()
	ctx.Bot.renameIDs[ctx.ChatID] = deviceID
	ctx.Bot.renameIDMu.Unlock()

	text := ctx.TDevice(msgRenamePrompt, deviceID)
	keyboard := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(msgRenameCancel)).WithCallbackData(fmt.Sprintf("rename_cancel:%s", deviceID)),
		),
	)
	ctx.sendWithKeyboard(text, keyboard)
}

func (ctx *RequestContext) cmdDeviceHistory(deviceID string) {
	ctx.clearLastPrompt()
	log.Debug().Int64("chat_id", ctx.ChatID).Str("device_id", deviceID).Msg("tgbot: cmdDeviceHistory start")

	_ = ctx.Bot.api.SendChatAction(context.Background(), &telego.SendChatActionParams{ChatID: tu.ID(ctx.ChatID), Action: "upload_photo"})

	history := ctx.Bot.monitor.GetHistory(deviceID)
	log.Debug().Int64("chat_id", ctx.ChatID).Int("count", len(history)).Msg("tgbot: history loaded")

	if len(history) == 0 {
		ctx.sendWithKeyboard(ctx.TDevice(msgHistoryEmpty, deviceID), ctx.mainKeyboard())
		return
	}

	log.Debug().Int64("chat_id", ctx.ChatID).Msg("tgbot: drawing charts start")
	buf, err := generateCharts(ctx, history, ctx.Bot.cfg.Chart.Width, ctx.Bot.cfg.Chart.Height, ctx.Bot.cfg.Chart.FontSize, chartSmoothingHistory)
	if err != nil {
		log.Error().Err(err).Int64("chat_id", ctx.ChatID).Msg("tgbot: failed to generate charts")
		ctx.sendWithKeyboard(ctx.T(msgHistoryError), ctx.mainKeyboard())
		return
	}
	log.Debug().Int64("chat_id", ctx.ChatID).Int("charts", len(buf)).Msg("tgbot: drawing charts end")

	var media []telego.InputMedia
	for i, b := range buf {
		nr := &bytesNamedReader{
			Reader: bytes.NewReader(b),
			name:   fmt.Sprintf("chart_%d.png", i),
		}

		photo := &telego.InputMediaPhoto{
			Type:  "photo",
			Media: tu.File(nr),
		}
		media = append(media, photo)
	}

	params := tu.MediaGroup(tu.ID(ctx.ChatID), media...)
	_, err = ctx.Bot.api.SendMediaGroup(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Int64("chat_id", ctx.ChatID).Msg("tgbot: failed to send media group")
		// Fallback to individual photos if media group fails
		for i, b := range buf {
			nr := &bytesNamedReader{Reader: bytes.NewReader(b), name: fmt.Sprintf("chart_%d.png", i)}
			p := &telego.SendPhotoParams{ChatID: tu.ID(ctx.ChatID), Photo: tu.File(nr)}
			_, _ = ctx.Bot.api.SendPhoto(context.Background(), p)
		}
	}

	// Send footer with keyboard
	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	deviceName := mcfg.DeviceNames[deviceID]
	if deviceName == "" {
		deviceName = deviceID
	}
	footer := ctx.T(msgHistoryFooter, map[string]interface{}{
		"count": len(history), "deviceId": deviceID, "deviceName": deviceName,
	})
	ctx.sendWithKeyboard(footer, ctx.mainKeyboard())
}

func (ctx *RequestContext) cmdResetConfirm() {
	ctx.clearLastPrompt()
	d := ctx.Bot.defaults
	std := strings.ToLower(d.AQI.Standard)
	stdLabel := ctx.T("standard" + strings.Title(std))

	unitT := ctx.T("txtUnit" + strings.Title(strings.ToLower(ctx.Bot.cfg.Default.Unit.Temp)))
	unitP := ctx.T("txtUnit" + strings.Title(strings.ToLower(ctx.Bot.cfg.Default.Unit.Press)))

	var alertsSB strings.Builder
	allAlerts := ctx.getAllAlerts("")

	defNotifications := make(map[string]bool)
	for _, n := range config.FlattenNotifications(d.Notifications) {
		defNotifications[n] = true
	}
	defWarnings := make(map[string]bool)
	for _, w := range config.FlattenNotifications(d.Warnings) {
		defWarnings[w] = true
	}

	for _, a := range allAlerts {
		if defNotifications[a.id] {
			statusIcon := ctx.Bot.TLang("en", kIcoSilent)
			if defWarnings[a.id] {
				statusIcon = ctx.Bot.TLang("en", kIcoLoud)
			}
			atoms := map[string]interface{}{
				"statusIcon": statusIcon,
				"pm":         a.pm,
				"action":     a.action,
				"icon":       a.actionIcon,
				"zone":       a.zone,
				"zoneIcon":   a.zoneIcon,
				"in":         ctx.T("txtLabelIn"),
				"delta":      a.delta,
				"aqiPrefix":  a.aqiPrefix,
				"aqiName":    a.aqiName,
				"isAqi":      strings.HasPrefix(a.id, "aqi_"),
				"isVal":      strings.HasPrefix(a.id, "val"),
				"isDiff":     strings.HasPrefix(a.id, "diff"),
			}

			if atoms["isAqi"].(bool) {
				atoms["name"] = fmt.Sprintf("%s: %s", a.aqiPrefix, a.aqiName)
			} else if atoms["isVal"].(bool) {
				atoms["name"] = ctx.T("alertValBtn", atoms)
			} else {
				atoms["name"] = ctx.T("alertDiffBtn", atoms)
			}

			line := ctx.T("msgResetAlertItem", atoms)
			alertsSB.WriteString(line)
			alertsSB.WriteByte('\n')
		}
	}

	lazyStr := func(v *int) string {
		if v == nil {
			return "-"
		}
		return fmt.Sprintf("%d", *v)
	}

	text := ctx.T("msgResetConfirm", map[string]interface{}{
		"l1_25": d.PM25.Level1, "l2_25": d.PM25.Level2, "dyn25": d.PM25.Diff,
		"l1_10": d.PM10.Level1, "l2_10": d.PM10.Level2, "dyn10": d.PM10.Diff,
		"lazyUp25": lazyStr(d.PM25.LazyNotify.Up), "lazyDown25": lazyStr(d.PM25.LazyNotify.Down),
		"lazyUp10": lazyStr(d.PM10.LazyNotify.Up), "lazyDown10": lazyStr(d.PM10.LazyNotify.Down),
		"lazyUpAqi": lazyStr(d.AQI.LazyNotify.Up), "lazyDownAqi": lazyStr(d.AQI.LazyNotify.Down),
		"stdName": stdLabel, "aqiStandardFlag": ctx.Bot.TLang("en", "icoFlag" + strings.ToUpper(std)),
		"unitT": unitT, "unitP": unitP,
		"alertsList": alertsSB.String(),
	})

	ctx.sendWithKeyboard(text, ctx.resetDefaultsKeyboard())
}

func (ctx *RequestContext) cmdResetExecute() {
	ctx.Bot.store.ResetSettings(ctx.ChatID, ctx.Bot.defaults)

	mcfg := ctx.Bot.store.GetSettings(ctx.ChatID, ctx.Bot.defaults)
	lazyStr := func(v *int) string {
		if v == nil {
			return "-"
		}
		return fmt.Sprintf("%d", *v)
	}

	text := ctx.T(msgResetExecution, map[string]interface{}{
		"l1_25": mcfg.PM25.Level1, "l2_25": mcfg.PM25.Level2, "dyn25": mcfg.PM25.Diff,
		"l1_10": mcfg.PM10.Level1, "l2_10": mcfg.PM10.Level2, "dyn10": mcfg.PM10.Diff,
		"lazyUp25": lazyStr(mcfg.PM25.LazyNotify.Up), "lazyDown25": lazyStr(mcfg.PM25.LazyNotify.Down),
		"lazyUp10": lazyStr(mcfg.PM10.LazyNotify.Up), "lazyDown10": lazyStr(mcfg.PM10.LazyNotify.Down),
		"lazyUpAqi": lazyStr(mcfg.AQI.LazyNotify.Up), "lazyDownAqi": lazyStr(mcfg.AQI.LazyNotify.Down),
	})

	ctx.sendWithKeyboard(text, ctx.settingsKeyboard())
}

func (ctx *RequestContext) cmdLazyMenu() {
	ctx.clearLastPrompt()
	text := ctx.T("msgLazyMenu")
	ctx.sendWithKeyboard(text, ctx.lazySettingsKeyboard())
}
