package tgbot

import (
	"context"
	"fmt"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/rs/zerolog/log"
)

func (b *Bot) mainKeyboard(chatID int64, deviceID ...string) *telego.InlineKeyboardMarkup {
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
			tu.InlineKeyboardButton(b.T(chatID, btnStatus)).WithCallbackData(cbStatus),
			tu.InlineKeyboardButton(b.T(chatID, btnSettings)).WithCallbackData(cbSettings),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnHistory)).WithCallbackData(cbHistory),
			tu.InlineKeyboardButton(b.T(chatID, btnCharts)).WithCallbackData(cbCharts),
		),
	)
}

func (b *Bot) settingsKeyboard(chatID int64) telego.ReplyMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSoundProfiles)).WithCallbackData(cmdSoundProfiles),
			tu.InlineKeyboardButton(b.T(chatID, btnSilentProfiles)).WithCallbackData(cmdSilentProfiles),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnAQISettings)).WithCallbackData(cmdAQISettings),
			tu.InlineKeyboardButton(b.T(chatID, btnThresholds)).WithCallbackData(cmdThresholdsMenu),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnList)).WithCallbackData(cmdList),
			tu.InlineKeyboardButton(b.T(chatID, btnResetDefaults)).WithCallbackData(cmdResetSettings),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnLang)).WithCallbackData(cmdLang),
			tu.InlineKeyboardButton(b.T(chatID, btnMainMenu)).WithCallbackData(cmdHelp),
		),
	)
}

func (b *Bot) resetDefaultsKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnYes)).WithCallbackData(cmdResetDefaultsYes),
			tu.InlineKeyboardButton(b.T(chatID, btnNo)).WithCallbackData(cmdSettings),
		),
	)
}

func (b *Bot) chartsMenuKeyboard(chatID int64, deviceID string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnChartAQI)).WithCallbackData(fmt.Sprintf("chart:aqi:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnChartPM)).WithCallbackData(fmt.Sprintf("chart:pm:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnChartTemp)).WithCallbackData(fmt.Sprintf("chart:temp:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnChartHum)).WithCallbackData(fmt.Sprintf("chart:hum:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnChartPress)).WithCallbackData(fmt.Sprintf("chart:press:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnMainMenu)).WithCallbackData(cmdHelp),
		),
	)
}

func (b *Bot) thresholdsKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Level1)).WithCallbackData(cmdPM25Level1),
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Level1)).WithCallbackData(cmdPM10Level1),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Level2)).WithCallbackData(cmdPM25Level2),
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Level2)).WithCallbackData(cmdPM10Level2),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Diff)).WithCallbackData(cmdPM25Diff),
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Diff)).WithCallbackData(cmdPM10Diff),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSetByAQI)).WithCallbackData(cmdAqiCycleMenu),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnBackSettings)).WithCallbackData(cmdSettings),
		),
	)
}

func (b *Bot) subscriptionKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSubscribe)).WithCallbackData(cmdSubscribe),
			tu.InlineKeyboardButton(b.T(chatID, btnUnsubscribe)).WithCallbackData(cmdUnsubscribe),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnBackSettings)).WithCallbackData(cmdSettings),
		),
	)
}

func (b *Bot) aqiSettingsKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	mcfg := b.GetUserSettings(chatID)
	std := strings.ToUpper(mcfg.AQIStandard)
	stdData, ok := sensor.Standards[std]
	if !ok {
		// Fallback if standard not found
		return nil
	}

	stdLabel := stdData.NameFull
	// Try localization
	stdLocKey := "standard" + strings.Title(strings.ToLower(std))
	if loc := b.T(chatID, stdLocKey); !strings.HasPrefix(loc, "!!") {
		stdLabel = loc
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

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, "btnAqiStandard", map[string]interface{}{
			"std": stdLabel, "aqiStandardFlag": stdData.Flag,
		})).WithCallbackData("aqi_std_toggle"),
	})

	for _, zone := range stdData.Zones {
		id := fmt.Sprintf("aqi_l%d", zone.Level)
		statusIcon := b.I(kIcoUnchecked)
		if activeNotifications[id] {
			statusIcon = b.I(kIcoChecked)
		}
		soundLabel := "btnWithoutSound"
		if loudWarnings[id] {
			soundLabel = "btnWithSound"
		}

		name := zone.Name
		nameKey := fmt.Sprintf("aqiNameL%d%s", zone.Level, strings.Title(strings.ToLower(std)))
		if loc := b.T(chatID, nameKey); !strings.HasPrefix(loc, "!!") {
			name = loc
		}

		btnText := b.T(chatID, "alertAqiBtn", map[string]interface{}{"icon": name, "label": zone.Icon})

		soundLabelStr := b.T(chatID, soundLabel)
		callbackData := fmt.Sprintf("aqi_sound:%s", id)
		if !activeNotifications[id] {
			soundLabelStr = b.T(chatID, "btnInactive")
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
		tu.InlineKeyboardButton(b.T(chatID, btnBackSettings)).WithCallbackData(cmdSettings),
	})

	return tu.InlineKeyboard(rows...)
}

func (b *Bot) notificationSettingsKeyboard(chatID int64, silent bool) *telego.InlineKeyboardMarkup {
	mcfg := b.GetUserSettings(chatID)
	filter := "val"
	if silent {
		filter = "diff"
	}
	allAlerts := b.getAllAlerts(chatID, filter)

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

		statusIcon := b.I(kIcoUnchecked)
		if activeNotifications[a.id] {
			statusIcon = b.I(kIcoChecked)
		}

		soundLabel := "btnWithoutSound"
		if activeWarnings[a.id] {
			soundLabel = "btnWithSound"
		}

		soundLabelStr := b.T(chatID, soundLabel)
		callbackData := fmt.Sprintf("toggle_sound:%s:%t", a.id, silent)
		if !activeNotifications[a.id] {
			soundLabelStr = b.T(chatID, "btnInactive")
			callbackData = "none"
		}

		var btnName string
		if strings.HasPrefix(a.id, "aqi_") {
			btnName = fmt.Sprintf("%s: %s", a.aqiPrefix, a.aqiName)
		} else if strings.HasPrefix(a.id, "val") {
			btnName = b.T(chatID, "alertValBtn", map[string]interface{}{
				"pm":   a.pm,
				"icon": a.actionIcon,
				"in":   b.T(chatID, "txtLabelIn"),
				"zone": a.zoneIcon,
			})
		} else {
			btnName = b.T(chatID, "alertDiffBtn", map[string]interface{}{
				"delta": a.delta,
				"pm":    a.pm,
				"icon":  a.actionIcon,
				"in":    b.T(chatID, "txtLabelIn"),
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
		tu.InlineKeyboardButton(b.T(chatID, btnBackSettings)).WithCallbackData(cmdSettings),
	})

	return tu.InlineKeyboard(rows...)
}

func (b *Bot) deviceInfoKeyboard(chatID int64, deviceID string) *telego.InlineKeyboardMarkup {
	return b.deviceSettingsKeyboard(chatID, deviceID)
}

func (b *Bot) deviceSettingsKeyboard(chatID int64, deviceID string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnRename)).WithCallbackData(fmt.Sprintf("rename:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnUnsubscribe)).WithCallbackData(fmt.Sprintf("unsub:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnList)).WithCallbackData(cmdList),
		),
	)
}

func (b *Bot) cancelThresholdKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnCancel)).WithCallbackData(cmdCancelThreshold),
		),
	)
}

func (b *Bot) cancelSubKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnCancel)).WithCallbackData(cmdCancelSub),
		),
	)
}

func (b *Bot) sendWithKeyboard(chatID int64, text string, markup telego.ReplyMarkup) {
	params := tu.Message(tu.ID(chatID), text).
		WithReplyMarkup(markup).
		WithParseMode(telego.ModeHTML)

	if markup != nil {
		b.clearLastPrompt(chatID)
	}

	msg, err := b.api.SendMessage(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Int64("chat_id", chatID).Str("text", text).Msg("tgbot: failed to send message")
	} else if markup != nil {
		b.setLastPrompt(chatID, msg.GetMessageID())
	}
}


