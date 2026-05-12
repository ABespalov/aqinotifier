package tgbot

import (
	"context"
	"fmt"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/rs/zerolog/log"
)

type AlertItem struct {
	id         string
	pm         string
	action     string
	actionIcon string
	zone       string
	zoneIcon   string
	delta      string
	aqiPrefix  string
	aqiName    string
	loud       bool
}

func (b *Bot) getAllAlerts(chatID int64, filter string) []AlertItem {
	d := b.defaults
	defWarnings := make(map[string]bool)
	for _, w := range d.Warnings {
		defWarnings[w] = true
	}

	var res []AlertItem

	// 1. AQI Alerts (aqi_z1 - aqi_z7)
	if filter == "" || filter == "aqi" {
		mcfg := b.GetUserSettings(chatID)
		std := strings.ToLower(mcfg.AQIStandard)
		for i := 1; i <= 7; i++ {
			if i > 6 && std == "eu" {
				continue // EU only has 6 zones
			}
			id := fmt.Sprintf("aqi_z%d", i)
			zoneIcon := b.I("icoAqi" + strings.ToUpper(std) + "Level" + fmt.Sprint(i))
			res = append(res, AlertItem{
				id:        id,
				aqiPrefix: b.T(chatID, "txtChartSubjectAqi"),
				aqiName:   b.T(chatID, "aqiNameZ"+fmt.Sprint(i)+strings.Title(std)),
				zoneIcon:  zoneIcon,
				loud:      defWarnings[id],
			})
		}
	}

	// 2. PM Value Alerts (valXX-XX)
	if filter == "" || filter == "val" || filter == "pm" {
		pmTypes := []string{"25", "10", "s"}
		actions := []string{"u", "d"}
		zones := []string{"g", "y", "r"}

		for _, pm := range pmTypes {
			for _, act := range actions {
				for _, zone := range zones {
					// Skip impossible combinations (e.g. up to green or down to red usually not tracked)
					if act == "u" && zone == "g" {
						continue
					}
					if act == "d" && zone == "r" {
						continue
					}

					id := fmt.Sprintf("val%s-%s%s", pm, zone, act)
					
					var pmLabel string
					switch pm {
					case "10":
						pmLabel = b.T(chatID, "alertV10Short")
					case "25":
						pmLabel = b.T(chatID, "alertV25Short")
					default:
						pmLabel = b.T(chatID, "alertVsShort")
					}

					actIcon := icoTrendUp
					if act == "d" {
						actIcon = icoTrendDown
					}

					var zoneIcon string
					switch zone {
					case "g":
						zoneIcon = icoPmLevel1
					case "y":
						zoneIcon = icoPmLevel2
					default:
						zoneIcon = icoPmLevel3
					}

					actName := b.T(chatID, "alertActionRise")
					if act == "d" {
						actName = b.T(chatID, "alertActionFall")
					}

					var zoneName string
					switch zone {
					case "g":
						zoneName = b.T(chatID, "labelZoneGreenAcc")
					case "y":
						zoneName = b.T(chatID, "labelZoneYellowAcc")
					default:
						zoneName = b.T(chatID, "labelZoneRedAcc")
					}

					res = append(res, AlertItem{
						id:         id,
						pm:         pmLabel,
						action:     actName,
						actionIcon: actIcon,
						zone:       zoneName,
						zoneIcon:   zoneIcon,
						loud:       defWarnings[id],
					})
				}
			}
		}
	}

	// 3. PM Dynamics Alerts (diffXX-XX)
	if filter == "" || filter == "diff" || filter == "pm" {
		pmTypes := []string{"25", "10", "s"}
		actions := []string{"u", "d"}
		zones := []string{"g", "y", "r"}
		for _, pm := range pmTypes {
			for _, act := range actions {
				for _, zone := range zones {
					id := fmt.Sprintf("diff%s-%s%s", pm, zone, act)
					
					var pmLabel string
					switch pm {
					case "10":
						pmLabel = b.T(chatID, "alertV10Short")
					case "25":
						pmLabel = b.T(chatID, "alertV25Short")
					default:
						pmLabel = b.T(chatID, "alertVsShort")
					}

					actIcon := icoTrendUp
					if act == "d" {
						actIcon = icoTrendDown
					}

					var zoneIcon string
					switch zone {
					case "g":
						zoneIcon = icoPmLevel1
					case "y":
						zoneIcon = icoPmLevel2
					default:
						zoneIcon = icoPmLevel3
					}

					actName := b.T(chatID, "alertActionRise")
					if act == "d" {
						actName = b.T(chatID, "alertActionFall")
					}

					var zoneName string
					switch zone {
					case "g":
						zoneName = b.T(chatID, "labelZoneGreenAcc")
					case "y":
						zoneName = b.T(chatID, "labelZoneYellowAcc")
					default:
						zoneName = b.T(chatID, "labelZoneRedAcc")
					}

					res = append(res, AlertItem{
						id:         id,
						pm:         pmLabel,
						action:     actName,
						actionIcon: actIcon,
						zone:       zoneName,
						zoneIcon:   zoneIcon,
						delta:      b.T(chatID, "txtLabelDelta"),
						loud:       defWarnings[id],
					})
				}
			}
		}
	}

	return res
}

func (b *Bot) handleCallback(cq *telego.CallbackQuery) {
	_ = b.api.AnswerCallbackQuery(context.Background(), tu.CallbackQuery(cq.ID))

	data := cq.Data
	if cq.Message == nil {
		return
	}
	chatID := cq.Message.GetChat().ID
	log.Debug().Int64("chat_id", chatID).Str("data", data).Msg("tgbot: received callback")

	switch {
	case data == "none":
		return
	case data == "menu_reset_defaults":
		b.cmdResetConfirm(chatID)
		return

	case data == "menu_main":
		if err := b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()}); err != nil {
			log.Error().Err(err).Int64("chat_id", chatID).Msg("tgbot: failed to delete message on menu_main")
		}
		b.sendHelp(chatID)
	case data == "menu_settings":
		if err := b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()}); err != nil {
			log.Error().Err(err).Int64("chat_id", chatID).Msg("tgbot: failed to delete message on menu_settings")
		}
		b.cmdSettings(chatID)
	case data == "menu_status":
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.cmdStatusMenu(chatID)
	case data == "menu_charts":
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.cmdChartsMenu(chatID)
	case data == "menu_history":
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
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
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.promptDeviceID(chatID)
	case data == "menu_unsubscribe":
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.cmdUnsubscribeMenu(chatID)
	case data == "menu_aqi":
		b.cmdAqiMenu(chatID, cq.Message.GetMessageID())
	case data == "menu_lang":
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.cmdLangMenu(chatID)
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
		b.sendWithKeyboard(chatID, b.T(chatID, "msgChartsMenu"), b.chartsMenuKeyboard(chatID, deviceID))

	case strings.HasPrefix(data, "pm_set:"):
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			b.promptThreshold(chatID, parts[1], parts[2])
		}
	case strings.HasPrefix(data, "unsub:"):
		deviceID := strings.TrimPrefix(data, "unsub:")
		text := b.TDevice(chatID, "msgUnsubConfirm", deviceID)
		kb := tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(b.T(chatID, btnYes, icoSuccess)).WithCallbackData(fmt.Sprintf("unsub_yes:%s", deviceID)),
				tu.InlineKeyboardButton(b.T(chatID, btnNo, icoError)).WithCallbackData(fmt.Sprintf("unsub_no:%s", deviceID)),
			),
		)
		params := tu.EditMessageText(tu.ID(chatID), cq.Message.GetMessageID(), text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.EditMessageText(context.Background(), params)

	case strings.HasPrefix(data, "unsub_yes:"):
		deviceID := strings.TrimPrefix(data, "unsub_yes:")
		b.store.Unsubscribe(chatID, deviceID)
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.sendWithKeyboard(chatID, b.TDevice(chatID, "msgUnsubscribed", deviceID), nil)
		b.cmdList(chatID)

	case strings.HasPrefix(data, "unsub_no:"):
		deviceID := strings.TrimPrefix(data, "unsub_no:")
		params := tu.EditMessageText(tu.ID(chatID), cq.Message.GetMessageID(), b.formatDeviceShortInfo(chatID, deviceID)).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(b.deviceInfoKeyboard(chatID, deviceID))
		_, _ = b.api.EditMessageText(context.Background(), params)

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
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.sendWithKeyboard(chatID, b.T(chatID, "msgHelp"), b.mainKeyboard(chatID))

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
	case strings.HasPrefix(data, "rename_cancel:"):
		deviceID := strings.TrimPrefix(data, "rename_cancel:")
		b.setState(chatID, stateIdle)
		b.renameIDMu.Lock()
		delete(b.renameIDs, chatID)
		b.renameIDMu.Unlock()
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.sendWithKeyboard(chatID, b.formatDeviceShortInfo(chatID, deviceID), b.deviceInfoKeyboard(chatID, deviceID))
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

		b.sendWithKeyboard(chatID, b.formatDeviceShortInfo(chatID, deviceID), b.deviceInfoKeyboard(chatID, deviceID))

	case strings.HasPrefix(data, "chart:"):
		parts := strings.SplitN(strings.TrimPrefix(data, "chart:"), ":", 2)
		if len(parts) == 2 {
			chartType := parts[0]
			deviceID := parts[1]
			b.sendChartForDevice(chatID, deviceID, chartType)
		}

	case strings.HasPrefix(data, "history:"):
		deviceID := strings.TrimPrefix(data, "history:")
		b.cmdDeviceHistory(chatID, deviceID)
	}
}
