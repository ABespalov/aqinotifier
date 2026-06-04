// Package tgbot implements the Telegram bot logic, command handlers, keyboards,
// and state storage.
// This file constructs inline and reply keyboard markups for menus, settings,
// chart selections, and confirmation prompts.
package tgbot

import (
	"context"
	"fmt"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/rs/zerolog/log"
)

func (ctx *RequestContext) mainKeyboard(deviceID ...string) *telego.InlineKeyboardMarkup {
	devID := ""
	if len(deviceID) > 0 {
		devID = deviceID[0]
	}

	cbStatus := cmdStatus
	cbSettings := cmdSettings
	cbHistory := cmdHistory
	cbCharts := cmdCharts

	if devID != "" {
		cbStatus = fmt.Sprintf("status:%s", devID)
		cbHistory = fmt.Sprintf("history:%s", devID)
	}

	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnStatus)).WithCallbackData(cbStatus),
			tu.InlineKeyboardButton(ctx.T(btnSettings)).WithCallbackData(cbSettings),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnHistory)).WithCallbackData(cbHistory),
			tu.InlineKeyboardButton(ctx.T(btnCharts)).WithCallbackData(cbCharts),
		),
	)
}

func (ctx *RequestContext) settingsKeyboard() telego.ReplyMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnSoundProfiles)).WithCallbackData(cmdSoundProfiles),
			tu.InlineKeyboardButton(ctx.T(btnSilentProfiles)).WithCallbackData(cmdSilentProfiles),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnAQISettings)).WithCallbackData(cmdAQISettings),
			tu.InlineKeyboardButton(ctx.T(btnThresholds)).WithCallbackData(cmdThresholdsMenu),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnLazySettings)).WithCallbackData("menu_lazy"),
			tu.InlineKeyboardButton(ctx.T(btnList)).WithCallbackData(cmdList),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnResetDefaults)).WithCallbackData(cmdResetSettings),
			tu.InlineKeyboardButton(ctx.T(btnLang)).WithCallbackData(cmdLang),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnMainMenu)).WithCallbackData(cmdHelp),
		),
	)
}

func (ctx *RequestContext) resetDefaultsKeyboard() *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnYes)).WithCallbackData(cmdResetDefaultsYes),
			tu.InlineKeyboardButton(ctx.T(btnNo)).WithCallbackData(cmdSettings),
		),
	)
}

func (ctx *RequestContext) chartsMenuKeyboard(deviceID string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnChartAQI)).WithCallbackData(fmt.Sprintf("chart:aqi:%s", deviceID)),
			tu.InlineKeyboardButton(ctx.T(btnChartPM)).WithCallbackData(fmt.Sprintf("chart:pm:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnChartTemp)).WithCallbackData(fmt.Sprintf("chart:temp:%s", deviceID)),
			tu.InlineKeyboardButton(ctx.T(btnChartHum)).WithCallbackData(fmt.Sprintf("chart:hum:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnChartPress)).WithCallbackData(fmt.Sprintf("chart:press:%s", deviceID)),
			tu.InlineKeyboardButton(ctx.T(btnMainMenu)).WithCallbackData(cmdHelp),
		),
	)
}

func (ctx *RequestContext) thresholdsKeyboard() *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnPM25Level1)).WithCallbackData(cmdPM25Level1),
			tu.InlineKeyboardButton(ctx.T(btnPM10Level1)).WithCallbackData(cmdPM10Level1),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnPM25Level2)).WithCallbackData(cmdPM25Level2),
			tu.InlineKeyboardButton(ctx.T(btnPM10Level2)).WithCallbackData(cmdPM10Level2),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnPM25Diff)).WithCallbackData(cmdPM25Diff),
			tu.InlineKeyboardButton(ctx.T(btnPM10Diff)).WithCallbackData(cmdPM10Diff),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnSetByAQI)).WithCallbackData(cmdAqiCycleMenu),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnBackSettings)).WithCallbackData(cmdSettings),
		),
	)
}

func (ctx *RequestContext) subscriptionKeyboard() *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnSubscribe)).WithCallbackData(cmdSubscribe),
			tu.InlineKeyboardButton(ctx.T(btnUnsubscribe)).WithCallbackData(cmdUnsubscribe),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnBackSettings)).WithCallbackData(cmdSettings),
		),
	)
}

func (ctx *RequestContext) aqiSettingsKeyboard() *telego.InlineKeyboardMarkup {
	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	std := strings.ToUpper(mcfg.AQI.Standard)
	stdData := sensor.GetStandard(std)
	if stdData == nil {
		// Fallback if standard not found
		return nil
	}

	stdLabel := stdData.NameFull
	// Try localization
	stdLocKey := "standard" + strings.Title(strings.ToLower(std))
	if loc := ctx.T(stdLocKey); !strings.HasPrefix(loc, "!!") {
		stdLabel = loc
	}

	activeNotifications := make(map[string]bool)
	for _, n := range config.FlattenNotifications(mcfg.Notifications) {
		activeNotifications[n] = true
	}
	loudWarnings := make(map[string]bool)
	for _, w := range config.FlattenNotifications(mcfg.Warnings) {
		loudWarnings[w] = true
	}

	var rows [][]telego.InlineKeyboardButton

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(ctx.T("btnAqiStandard", map[string]interface{}{
			"std": stdLabel, "aqiStandardFlag": stdData.Flag,
		})).WithCallbackData("aqi_std_toggle"),
	})

	for _, zone := range stdData.Zones {
		id := fmt.Sprintf("aqi_l%d", zone.Level)
		statusIcon := ctx.Bot.I(kIcoUnchecked)
		if activeNotifications[id] {
			statusIcon = ctx.Bot.I(kIcoChecked)
		}
		soundLabel := "btnWithoutSound"
		if loudWarnings[id] {
			soundLabel = "btnWithSound"
		}

		name := zone.Name
		nameKey := fmt.Sprintf("aqiNameL%d%s", zone.Level, strings.Title(strings.ToLower(std)))
		if loc := ctx.T(nameKey); !strings.HasPrefix(loc, "!!") {
			name = loc
		}

		btnText := ctx.T("alertAqiBtn", map[string]interface{}{"icon": name, "label": zone.Icon})

		soundLabelStr := ctx.T(soundLabel)
		callbackData := fmt.Sprintf("aqi_sound:%s", id)
		if !activeNotifications[id] {
			soundLabelStr = ctx.T("btnInactive")
			callbackData = "none"
		}

		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", statusIcon, btnText)).
				WithCallbackData(fmt.Sprintf("aqi_toggle:%s", id)),
			tu.InlineKeyboardButton(soundLabelStr).
				WithCallbackData(callbackData),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(ctx.T(btnBackSettings)).WithCallbackData(cmdSettings),
	})

	return tu.InlineKeyboard(rows...)
}

func (ctx *RequestContext) notificationSettingsKeyboard(silent bool) *telego.InlineKeyboardMarkup {
	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	filter := "val"
	if silent {
		filter = "diff"
	}
	allAlerts := ctx.getAllAlerts(filter)

	activeNotifications := make(map[string]bool)
	for _, n := range config.FlattenNotifications(mcfg.Notifications) {
		activeNotifications[n] = true
	}

	activeWarnings := make(map[string]bool)
	for _, w := range config.FlattenNotifications(mcfg.Warnings) {
		activeWarnings[w] = true
	}

	var rows [][]telego.InlineKeyboardButton
	for _, a := range allAlerts {
		statusIcon := ctx.Bot.I(kIcoUnchecked)
		if activeNotifications[a.id] {
			statusIcon = ctx.Bot.I(kIcoChecked)
		}

		soundLabel := "btnWithoutSound"
		if activeWarnings[a.id] {
			soundLabel = "btnWithSound"
		}

		soundLabelStr := ctx.T(soundLabel)
		callbackData := fmt.Sprintf("toggle_sound:%s:%t", a.id, silent)
		if !activeNotifications[a.id] {
			soundLabelStr = ctx.T("btnInactive")
			callbackData = "none"
		}

		var btnName string
		if strings.HasPrefix(a.id, "aqi_") {
			btnName = fmt.Sprintf("%s: %s", a.aqiPrefix, a.aqiName)
		} else if strings.HasPrefix(a.id, "val") {
			btnName = ctx.T("alertValBtn", map[string]interface{}{
				"pm":   a.pm,
				"icon": a.actionIcon,
				"in":   ctx.T("txtLabelIn"),
				"zone": a.zoneIcon,
			})
		} else {
			btnName = ctx.T("alertDiffBtn", map[string]interface{}{
				"delta": a.delta,
				"pm":    a.pm,
				"icon":  a.actionIcon,
				"in":    ctx.T("txtLabelIn"),
				"zone":  a.zoneIcon,
			})
		}

		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", statusIcon, btnName)).
				WithCallbackData(fmt.Sprintf("toggle:%s:%t", a.id, silent)),
			tu.InlineKeyboardButton(soundLabelStr).
				WithCallbackData(callbackData),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(ctx.T(btnBackSettings)).WithCallbackData(cmdSettings),
	})

	return tu.InlineKeyboard(rows...)
}

func (ctx *RequestContext) deviceInfoKeyboard(deviceID string) *telego.InlineKeyboardMarkup {
	return ctx.deviceSettingsKeyboard(deviceID)
}

func (ctx *RequestContext) deviceSettingsKeyboard(deviceID string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnRename)).WithCallbackData(fmt.Sprintf("rename:%s", deviceID)),
			tu.InlineKeyboardButton(ctx.T(btnUnsubscribe)).WithCallbackData(fmt.Sprintf("unsub:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnBackDevices)).WithCallbackData(cmdList),
		),
	)
}

func (ctx *RequestContext) cancelThresholdKeyboard() *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnCancel)).WithCallbackData(cmdCancelThreshold),
		),
	)
}

func (ctx *RequestContext) cancelSubKeyboard() *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnCancel)).WithCallbackData(cmdCancelSub),
		),
	)
}

func (ctx *RequestContext) lazySettingsKeyboard() *telego.InlineKeyboardMarkup {
	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	aqiUpVal := 2
	if mcfg.AQI.LazyNotify.Up != nil {
		aqiUpVal = *mcfg.AQI.LazyNotify.Up
	}
	aqiDownVal := 3
	if mcfg.AQI.LazyNotify.Down != nil {
		aqiDownVal = *mcfg.AQI.LazyNotify.Down
	}

	pm10UpVal := 2
	if mcfg.PM10.LazyNotify.Up != nil {
		pm10UpVal = *mcfg.PM10.LazyNotify.Up
	}
	pm10DownVal := 3
	if mcfg.PM10.LazyNotify.Down != nil {
		pm10DownVal = *mcfg.PM10.LazyNotify.Down
	}

	pm25UpVal := 2
	if mcfg.PM25.LazyNotify.Up != nil {
		pm25UpVal = *mcfg.PM25.LazyNotify.Up
	}
	pm25DownVal := 3
	if mcfg.PM25.LazyNotify.Down != nil {
		pm25DownVal = *mcfg.PM25.LazyNotify.Down
	}

	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T("btnAqiLazyUp", map[string]interface{}{"val": aqiUpVal})).WithCallbackData("lazy_set:aqi:up"),
			tu.InlineKeyboardButton(ctx.T("btnAqiLazyDown", map[string]interface{}{"val": aqiDownVal})).WithCallbackData("lazy_set:aqi:down"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnPm25LazyUp, map[string]interface{}{"val": pm25UpVal})).WithCallbackData("lazy_set:pm25:up"),
			tu.InlineKeyboardButton(ctx.T(btnPm25LazyDown, map[string]interface{}{"val": pm25DownVal})).WithCallbackData("lazy_set:pm25:down"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnPm10LazyUp, map[string]interface{}{"val": pm10UpVal})).WithCallbackData("lazy_set:pm10:up"),
			tu.InlineKeyboardButton(ctx.T(btnPm10LazyDown, map[string]interface{}{"val": pm10DownVal})).WithCallbackData("lazy_set:pm10:down"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnBackSettings)).WithCallbackData(cmdSettings),
		),
	)
}

func (ctx *RequestContext) cancelLazyKeyboard() *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(ctx.T(btnCancel)).WithCallbackData("cancel_lazy"),
		),
	)
}

func (ctx *RequestContext) sendWithKeyboard(text string, markup telego.ReplyMarkup) {
	params := tu.Message(tu.ID(ctx.ChatID), text).
		WithReplyMarkup(markup).
		WithParseMode(telego.ModeHTML)

	if markup != nil {
		ctx.clearLastPrompt()
	}

	msg, err := ctx.Bot.api.SendMessage(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Int64("chat_id", ctx.ChatID).Str("text", text).Msg("tgbot: failed to send message")
	} else if markup != nil {
		ctx.setLastPrompt(msg.GetMessageID())
	}
}
