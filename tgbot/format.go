// Package tgbot implements the Telegram bot logic, command handlers, keyboards,
// and state storage.
// This file provides text formatting functions for device statuses, measurements,
// AQI values, and localized unit labels.
package tgbot

import (
	"fmt"
	"strings"

	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/rs/zerolog/log"
)

// getEventPriority returns a priority score for a given alert event ID.
// Higher score = shown as the "winner" (most prominent event) in a notification.
// Event IDs use underscores, e.g. "vals_l2u", "val25_l3u", "aqi_l1".
func (b *Bot) getEventPriority(id string) int {
	switch {
	case strings.HasPrefix(id, "aqi_"):
		return 70
	case strings.HasPrefix(id, "vals_"):
		return 60
	case strings.HasPrefix(id, "val25_"):
		return 50
	case strings.HasPrefix(id, "val10_"):
		return 40
	case strings.HasPrefix(id, "diffs_"):
		return 30
	case strings.HasPrefix(id, "diff25_"):
		return 20
	case strings.HasPrefix(id, "diff10_"):
		return 10
	default:
		return 0
	}
}
func (b *Bot) getAQIIcon(level sensor.AQILevel, standard string) string {
	std, ok := sensor.Standards[strings.ToUpper(standard)]
	if ok && int(level) > 0 && int(level) <= len(std.Zones) {
		return b.Resolve(std.Zones[level-1].Icon)
	}

	// Fallback to old iconsMap if dynamic fails
	key := fmt.Sprintf("icoAqi%sLevel%d", strings.ToUpper(standard), level)
	return b.I(key)
}
func (b *Bot) formatDeviceStatus(chatID int64, deviceID string) string {
	log.Debug().Int64("chat_id", chatID).Str("device_id", deviceID).Msg("tgbot: formatDeviceStatus start")
	m := b.monitor.LastMeasurement(deviceID)
	if m == nil {
		log.Debug().Int64("chat_id", chatID).Str("device_id", deviceID).Msg("tgbot: no measurement found")
		return b.TDevice(chatID, "msgStatusNoData", deviceID)
	}
	res := b.formatMeasurement(chatID, m)
	log.Debug().Int64("chat_id", chatID).Int("len", len(res)).Msg("tgbot: formatDeviceStatus end")
	return res
}
func (b *Bot) formatDeviceIDPlain(chatID int64, deviceID string) string {
	mcfg := b.GetUserSettings(chatID)
	name, ok := mcfg.DeviceNames[deviceID]
	if ok && name != "" {
		return fmt.Sprintf("%s (%s)", name, deviceID)
	}
	return fmt.Sprintf("%s %s", b.T(chatID, "msgDevice"), deviceID)
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
		return b.T(chatID, "unitMmhg")
	}
	return b.T(chatID, "unitHpa")
}

func (b *Bot) buildMeasurementArgs(chatID int64, m *monitor.Measurement) map[string]interface{} {
	mcfg := b.GetUserSettings(chatID)

	args := map[string]interface{}{
		"date": m.Timestamp,
		"time": m.Timestamp,

		"deviceId": m.DeviceID,

		"val25":     m.PM25,
		"prev25":    m.PM25Prev,
		"curr25":    m.PM25,
		"unitPm":    b.T(chatID, "unitPm"),
		"val10":     m.PM10,
		"prev10":    m.PM10Prev,
		"curr10":    m.PM10,
		"labelPm25": b.T(chatID, "labelPm25"),
		"labelPm10": b.T(chatID, "labelPm10"),
		"l1_25":     mcfg.PM25.Level1,
		"l2_25":     mcfg.PM25.Level2,
		"l1_10":     mcfg.PM10.Level1,
		"l2_10":     mcfg.PM10.Level2,
	}

	if name, ok := mcfg.DeviceNames[m.DeviceID]; ok && name != "" {
		args["deviceName"] = name
	} else {
		args["deviceName"] = b.T(chatID, "msgDevice") + " " + m.DeviceID
	}

	if m.PM25Diff != nil {
		args["diff25Percent"] = *m.PM25Diff
	} else {
		args["diff25Percent"] = 0.0
	}
	if m.PM10Diff != nil {
		args["diff10Percent"] = *m.PM10Diff
	} else {
		args["diff10Percent"] = 0.0
	}

	diff25 := m.PM25 - m.PM25Prev
	if diff25 > 0 {
		args["trend25Icon"] = b.I(kIcoTrendUp)
	} else if diff25 < 0 {
		args["trend25Icon"] = b.I(kIcoTrendDown)
	} else {
		args["trend25Icon"] = b.I(kIcoTrendFlat)
	}
	args["diff25"] = diff25

	diff10 := m.PM10 - m.PM10Prev
	if diff10 > 0 {
		args["trend10Icon"] = b.I(kIcoTrendUp)
	} else if diff10 < 0 {
		args["trend10Icon"] = b.I(kIcoTrendDown)
	} else {
		args["trend10Icon"] = b.I(kIcoTrendFlat)
	}
	args["diff10"] = diff10

	getZoneIcon := func(v, g, y float64) string {
		if v <= g {
			return b.I(kIcoPmLevel1)
		}
		if v <= y {
			return b.I(kIcoPmLevel2)
		}
		return b.I(kIcoPmLevel3)
	}
	args["zone25Icon"] = getZoneIcon(m.PM25, mcfg.PM25.Level1, mcfg.PM25.Level2)
	args["zone10Icon"] = getZoneIcon(m.PM10, mcfg.PM10.Level1, mcfg.PM10.Level2)

	var aqi float64
	var level sensor.AQILevel
	aqi, level = sensor.CalculateValueAQI(m.PM25, "PM2.5", mcfg.AQI.Standard)
	aqi10, level10 := sensor.CalculateValueAQI(m.PM10, "PM10", mcfg.AQI.Standard)
	if aqi10 > aqi {
		aqi = aqi10
		level = level10
	}

	levelChar := fmt.Sprintf("l%d", level)
	args["aqiIcon"] = b.getAQIIcon(level, mcfg.AQI.Standard)
	args["aqiVal"] = aqi
	args["aqiLevel"] = int(level)

	stdTag := strings.ToUpper(mcfg.AQI.Standard)
	aqiName := ""
	if s, ok := sensor.Standards[stdTag]; ok && int(level) > 0 && int(level) <= len(s.Zones) {
		aqiName = s.Zones[level-1].Name
	}
	// Try localization if available
	key := fmt.Sprintf("aqiName%s%s", strings.Title(levelChar), strings.Title(strings.ToLower(mcfg.AQI.Standard)))
	if localized := b.T(chatID, key); !strings.HasPrefix(localized, "!!") {
		aqiName = localized
	}
	args["aqiName"] = aqiName
	args["aqiStandardFlag"] = "{icoFlag" + stdTag + "}"
	if s, ok := sensor.Standards[stdTag]; ok && s.Flag != "" {
		args["aqiStandardFlag"] = s.Flag
	}

	log.Debug().Interface("args", args).Msg("tgbot: measurement args built")

	if m.Temperature != 0 {
		args["labelT"] = b.T(chatID, "msgTemp")
		args["valT"] = b.convertTemp(m.Temperature, chatID)
		args["unitT"] = b.unitTempLabel(chatID)
	}
	if m.Humidity != 0 {
		args["labelH"] = b.T(chatID, "msgHum")
		args["valH"] = m.Humidity
		if m.Temperature != 0 {
			dp := CalcDewPoint(m.Temperature, m.Humidity)
			args["labelDp"] = b.T(chatID, "msgDewPoint")
			args["valDp"] = b.convertTemp(dp, chatID)
		}
	}
	if m.Pressure != 0 {
		args["labelP"] = b.T(chatID, "msgPress")
		args["valP"] = b.convertPress(m.Pressure, chatID)
		args["unitP"] = b.unitPressLabel(chatID)
	}

	return args
}

func (b *Bot) formatMeasurement(chatID int64, m *monitor.Measurement) string {
	args := b.buildMeasurementArgs(chatID, m)
	return b.T(chatID, "msgStatus", args)
}
