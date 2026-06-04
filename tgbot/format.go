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
	std := sensor.GetStandard(standard)
	if std != nil && int(level) > 0 && int(level) <= len(std.Zones) {
		return b.Resolve(std.Zones[level-1].Icon)
	}

	// Fallback to old iconsMap if dynamic fails
	key := fmt.Sprintf("icoAqi%sLevel%d", strings.ToUpper(standard), level)
	return b.I(key)
}
func (ctx *RequestContext) formatDeviceStatus(deviceID string) string {
	log.Debug().Int64("chat_id", ctx.ChatID).Str("device_id", deviceID).Msg("tgbot: formatDeviceStatus start")
	m := ctx.Bot.monitor.LastMeasurement(deviceID)
	if m == nil {
		log.Debug().Int64("chat_id", ctx.ChatID).Str("device_id", deviceID).Msg("tgbot: no measurement found")
		return ctx.TDevice("msgStatusNoData", deviceID)
	}
	res := ctx.formatMeasurement(m)
	log.Debug().Int64("chat_id", ctx.ChatID).Int("len", len(res)).Msg("tgbot: formatDeviceStatus end")
	return res
}
func (ctx *RequestContext) formatDeviceIDPlain(deviceID string) string {
	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	name, ok := mcfg.DeviceNames[deviceID]
	if ok && name != "" {
		return fmt.Sprintf("%s (%s)", name, deviceID)
	}
	return fmt.Sprintf("%s %s", ctx.T("msgDevice"), deviceID)
}
func (ctx *RequestContext) convertTemp(celsius float64) float64 {
	unit := ctx.Bot.store.GetUnitTemp(ctx.ChatID)
	if unit == "f" {
		return celsius*1.8 + 32
	}
	return celsius
}
func (ctx *RequestContext) convertPress(hpa float64) float64 {
	unit := ctx.Bot.store.GetUnitPress(ctx.ChatID)
	if unit == "mmhg" {
		return hpa * hPaToMmHg
	}
	return hpa
}
func (ctx *RequestContext) unitTempLabel() string {
	unit := ctx.Bot.store.GetUnitTemp(ctx.ChatID)
	if unit == "f" {
		return "°F"
	}
	return "°C"
}
func (ctx *RequestContext) unitPressLabel() string {
	unit := ctx.Bot.store.GetUnitPress(ctx.ChatID)
	if unit == "mmhg" {
		return ctx.T("unitMmhg")
	}
	return ctx.T("unitHpa")
}

func (ctx *RequestContext) buildMeasurementArgs(m *monitor.Measurement) map[string]interface{} {
	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)

	args := map[string]interface{}{
		"date": m.Timestamp,
		"time": m.Timestamp,

		"deviceId": m.DeviceID,

		"val25":     m.PM25,
		"prev25":    m.PM25Prev,
		"curr25":    m.PM25,
		"unitPm":    ctx.T("unitPm"),
		"val10":     m.PM10,
		"prev10":    m.PM10Prev,
		"curr10":    m.PM10,
		"labelPm25": ctx.T("labelPm25"),
		"labelPm10": ctx.T("labelPm10"),
		"l1_25":     mcfg.PM25.Level1,
		"l2_25":     mcfg.PM25.Level2,
		"l1_10":     mcfg.PM10.Level1,
		"l2_10":     mcfg.PM10.Level2,
	}

	if name, ok := mcfg.DeviceNames[m.DeviceID]; ok && name != "" {
		args["deviceName"] = name
	} else {
		args["deviceName"] = ctx.T("msgDevice") + " " + m.DeviceID
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
		args["trend25Icon"] = ctx.Bot.I(kIcoTrendUp)
	} else if diff25 < 0 {
		args["trend25Icon"] = ctx.Bot.I(kIcoTrendDown)
	} else {
		args["trend25Icon"] = ctx.Bot.I(kIcoTrendFlat)
	}
	args["diff25"] = diff25

	diff10 := m.PM10 - m.PM10Prev
	if diff10 > 0 {
		args["trend10Icon"] = ctx.Bot.I(kIcoTrendUp)
	} else if diff10 < 0 {
		args["trend10Icon"] = ctx.Bot.I(kIcoTrendDown)
	} else {
		args["trend10Icon"] = ctx.Bot.I(kIcoTrendFlat)
	}
	args["diff10"] = diff10

	getZoneIcon := func(v, g, y float64) string {
		if v <= g {
			return ctx.Bot.I(kIcoPmLevel1)
		}
		if v <= y {
			return ctx.Bot.I(kIcoPmLevel2)
		}
		return ctx.Bot.I(kIcoPmLevel3)
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
	args["aqiIcon"] = ctx.Bot.getAQIIcon(level, mcfg.AQI.Standard)
	args["aqiVal"] = aqi
	args["aqiLevel"] = int(level)

	stdTag := strings.ToUpper(mcfg.AQI.Standard)
	aqiName := ""
	if s := sensor.GetStandard(stdTag); s != nil && int(level) > 0 && int(level) <= len(s.Zones) {
		aqiName = s.Zones[level-1].Name
	}
	// Try localization if available
	key := fmt.Sprintf("aqiName%s%s", strings.Title(levelChar), strings.Title(strings.ToLower(mcfg.AQI.Standard)))
	if localized := ctx.T(key); !strings.HasPrefix(localized, "!!") {
		aqiName = localized
	}
	args["aqiName"] = aqiName
	args["aqiStandardFlag"] = "{icoFlag" + stdTag + "}"
	if s := sensor.GetStandard(stdTag); s != nil && s.Flag != "" {
		args["aqiStandardFlag"] = s.Flag
	}

	log.Debug().Interface("args", args).Msg("tgbot: measurement args built")

	if m.Temperature != 0 {
		args["labelT"] = ctx.T("msgTemp")
		args["valT"] = ctx.convertTemp(m.Temperature)
		args["unitT"] = ctx.unitTempLabel()
	}
	if m.Humidity != 0 {
		args["labelH"] = ctx.T("msgHum")
		args["valH"] = m.Humidity
		if m.Temperature != 0 {
			dp := CalcDewPoint(m.Temperature, m.Humidity)
			args["labelDp"] = ctx.T("msgDewPoint")
			args["valDp"] = ctx.convertTemp(dp)
		}
	}
	if m.Pressure != 0 {
		args["labelP"] = ctx.T("msgPress")
		args["valP"] = ctx.convertPress(m.Pressure)
		args["unitP"] = ctx.unitPressLabel()
	}

	return args
}

func (ctx *RequestContext) formatMeasurement(m *monitor.Measurement) string {
	args := ctx.buildMeasurementArgs(m)
	return ctx.T("msgStatus", args)
}
