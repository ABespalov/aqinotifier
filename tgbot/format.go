package tgbot

import (
	"fmt"
	"strings"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
)

func (b *Bot) getEventPriority(id string) int {
	switch {
	case strings.HasPrefix(id, "aqi_"):
		return 70
	case strings.HasPrefix(id, "vals-"):
		return 60
	case strings.HasPrefix(id, "val25-"):
		return 50
	case strings.HasPrefix(id, "val10-"):
		return 40
	case strings.HasPrefix(id, "diffs-"):
		return 30
	case strings.HasPrefix(id, "diff25-"):
		return 20
	case strings.HasPrefix(id, "diff10-"):
		return 10
	default:
		return 0
	}
}
func (b *Bot) getEventHeader(chatID int64, id string) (string, string) {

	if id == "aqi_z1" || strings.HasSuffix(id, "-gd") {
		return IconPlant, b.T(chatID, "msg_norma")
	}

	if strings.HasSuffix(id, "-yd") || strings.HasSuffix(id, "-rd") {
		return IconInfo, b.T(chatID, "msg_info")
	}

	return IconAlert, b.T(chatID, "msg_alert")
}
func (b *Bot) getEventDescription(chatID int64, id string) string {
	pm10 := "@alert_pm10@"
	pm25 := "@alert_pm25@"
	pms := "@alert_pms@"

	zAccG := "@alert_short_zone_acc_g@"
	zAccY := "@alert_short_zone_acc_y@"
	zAccR := "@alert_short_zone_acc_r@"
	zPreG := "@alert_short_zone_pre_g@"
	zPreY := "@alert_short_zone_pre_y@"
	zPreR := "@alert_short_zone_pre_r@"

	riseIn := func(pm, zone string) string {
		return b.T(chatID, "alert_pm_rise_in", map[string]interface {
		}{"pm": pm, "zone": zone})
	}
	fallIn := func(pm, zone string) string {
		return b.T(chatID, "alert_pm_fall_in", map[string]interface {
		}{"pm": pm, "zone": zone})
	}
	riseTo := func(pm, zone string) string {
		return b.T(chatID, "alert_pm_rise_to", map[string]interface {
		}{"pm": pm, "zone": zone})
	}
	fallTo := func(pm, zone string) string {
		return b.T(chatID, "alert_pm_fall_to", map[string]interface {
		}{"pm": pm, "zone": zone})
	}
	norm := func(pm, zone string) string {
		return b.T(chatID, "alert_pm_return", map[string]interface {
		}{"pm": pm, "zone": zone})
	}

	switch id {

	case "val10-yu":
		return riseTo(pm10, zAccY)
	case "val10-ru":
		return riseTo(pm10, zAccR)
	case "val10-yd":
		return fallTo(pm10, zAccY)
	case "val10-gd":
		return norm(pm10, zAccG)

	case "val25-yu":
		return riseTo(pm25, zAccY)
	case "val25-ru":
		return riseTo(pm25, zAccR)
	case "val25-yd":
		return fallTo(pm25, zAccY)
	case "val25-gd":
		return norm(pm25, zAccG)

	case "vals-yu":
		return riseTo(pms, zAccY)
	case "vals-ru":
		return riseTo(pms, zAccR)
	case "vals-yd":
		return fallTo(pms, zAccY)
	case "vals-gd":
		return norm(pms, zAccG)

	case "diff10-gu":
		return riseIn(pm10, zPreG)
	case "diff10-yu":
		return riseIn(pm10, zPreY)
	case "diff10-ru":
		return riseTo(pm10, zAccR)
	case "diff10-gd":
		return norm(pm10, zAccG)
	case "diff10-yd":
		return fallTo(pm10, zAccY)
	case "diff10-rd":
		return fallIn(pm10, zPreR)

	case "diff25-gu":
		return riseIn(pm25, zPreG)
	case "diff25-yu":
		return riseIn(pm25, zPreY)
	case "diff25-ru":
		return riseTo(pm25, zAccR)
	case "diff25-gd":
		return norm(pm25, zAccG)
	case "diff25-yd":
		return fallTo(pm25, zAccY)
	case "diff25-rd":
		return fallIn(pm25, zPreR)

	case "diffs-gu":
		return riseIn(pms, zPreG)
	case "diffs-yu":
		return riseIn(pms, zPreY)
	case "diffs-ru":
		return riseTo(pms, zAccR)
	case "diffs-gd":
		return norm(pms, zAccG)
	case "diffs-yd":
		return fallTo(pms, zAccY)
	case "diffs-rd":
		return fallIn(pms, zPreR)
	}

	if strings.HasPrefix(id, "aqi_") {
		levelChar := strings.TrimPrefix(id, "aqi_")
		mcfg := b.GetUserSettings(chatID)
		std := strings.ToLower(mcfg.AQIStandard)
		name := b.T(chatID, "aqi_name_"+levelChar+"_"+std)

		if id == "aqi_z1" {
			return b.T(chatID, "alert_aqi_return")
		}

		return b.T(chatID, "alert_aqi_rise", map[string]interface {
		}{"zone": name})
	}

	return ""
}
func (b *Bot) getAQIIcon(level sensor.AQILevel, standard string) string {
	if strings.ToUpper(standard) == "US" {
		switch level {
		case sensor.LevelGood:
			return IconGreen
		case sensor.LevelModerate:
			return IconYellow
		case sensor.LevelSlightlyUnhealthy:
			return IconOrange
		case sensor.LevelUnhealthy:
			return IconRed
		case sensor.LevelVeryUnhealthy:
			return IconPurple
		case sensor.LevelHazardous:
			return IconMaroon
		case sensor.LevelExtremelyHazardous:
			return IconBlack
		}
	} else {
		switch level {
		case sensor.LevelGood:
			return IconBlue
		case sensor.LevelModerate:
			return IconGreen
		case sensor.LevelSlightlyUnhealthy:
			return IconYellow
		case sensor.LevelUnhealthy:
			return IconOrange
		case sensor.LevelVeryUnhealthy:
			return IconRed
		case sensor.LevelHazardous, sensor.LevelExtremelyHazardous:
			return IconMaroon
		}
	}
	return IconUnknown
}
func (b *Bot) formatDeviceStatus(chatID int64, deviceID string) string {
	m := b.monitor.LastMeasurement(deviceID)
	if m == nil {
		return b.T(chatID, "status_no_data", map[string]interface {
		}{"device_id": b.formatDeviceID(chatID, deviceID)})
	}
	return b.formatMeasurement(chatID, m)
}
func (b *Bot) formatDeviceShortInfo(chatID int64, deviceID string) string {
	mcfg := b.GetUserSettings(chatID)
	name, ok := mcfg.DeviceNames[deviceID]
	if !ok || name == "" {
		name = b.T(chatID, "msg_device") + " " + deviceID
	}
	header := b.T(chatID, "msg_device_info_header")
	return fmt.Sprintf("%s\n\n<code>%s</code>\n%s", header, deviceID, name)
}
func (b *Bot) formatDeviceID(chatID int64, deviceID string) string {
	mcfg := b.GetUserSettings(chatID)
	name, ok := mcfg.DeviceNames[deviceID]
	if ok && name != "" {
		return fmt.Sprintf("%s %s (<code>%s</code>)", IconDevice, name, deviceID)
	}
	return fmt.Sprintf("%s %s <code>%s</code>", IconDevice, b.T(chatID, "msg_device"), deviceID)
}
func (b *Bot) formatDeviceIDPlain(chatID int64, deviceID string) string {
	mcfg := b.GetUserSettings(chatID)
	name, ok := mcfg.DeviceNames[deviceID]
	if ok && name != "" {
		return fmt.Sprintf("%s (%s)", name, deviceID)
	}
	return fmt.Sprintf("%s %s", b.T(chatID, "msg_device"), deviceID)
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
		return b.T(chatID, "msg_unit_mmhg")
	}
	return b.T(chatID, "unit_hpa")
}
func (b *Bot) formatMeasurement(chatID int64, m *monitor.Measurement) string {
	var sb strings.Builder
	t := m.Timestamp.Local()
	sb.WriteString(b.T(chatID, "msg_status_header", map[string]interface{}{
		"date": t, 
		"time": t,
	}))

	sb.WriteString(strings.TrimSpace(b.formatAQILine(chatID, m, true)))
	sb.WriteString("\n\n")
	sb.WriteString(b.formatPMStatusLine(chatID, m, "PM2.5"))
	sb.WriteString("\n\n")
	sb.WriteString(b.formatPMStatusLine(chatID, m, "PM10"))
	sb.WriteString("\n\n")
	sb.WriteString(strings.TrimSpace(b.formatWeatherLines(chatID, m)))
	sb.WriteString("\n\n")
	sb.WriteString(strings.TrimSpace(b.formatFooter(chatID, m)))

	return sb.String()
}
func (b *Bot) formatAQILine(chatID int64, m *monitor.Measurement, bold bool) string {
	mcfg := b.GetUserSettings(chatID)
	var aqi float64
	var level sensor.AQILevel
	std := strings.ToLower(mcfg.AQIStandard)
	if mcfg.AQIStandard == "US" {
		aqi, level = sensor.CalculateUS_AQI(m.PM25, m.PM10)
	} else {
		aqi, level = sensor.CalculateEU_AQI(m.PM25, m.PM10)
	}

	aqiIcon := b.getAQIIcon(level, mcfg.AQIStandard)
	levelChar := fmt.Sprintf("z%d", level)
	aqiName := b.T(chatID, "aqi_name_"+levelChar+"_"+std)
	flag := "@flag_" + strings.ToLower(std) + "@"

	if bold {
		return b.T(chatID, "msg_status_aqi", map[string]interface {
		}{"icon": aqiIcon, "aqi_val": aqi, "aqi_name": aqiName, "aqi_standard_flag": flag})
	}

	return fmt.Sprintf("%s AQI: %.1f — %s %s", aqiIcon, aqi, aqiName, b.T(chatID, "flag_"+strings.ToLower(std)))
}
func (b *Bot) formatPMStatusLine(chatID int64, m *monitor.Measurement, pmType string) string {
	mcfg := b.GetUserSettings(chatID)
	var val, prev float64
	var pcent *float64
	var icon, label string
	var g, y float64

	if pmType == "PM10" {
		val, prev = m.PM10, m.PM10Prev
		pcent = m.PM10Diff
		icon, label = IconPM10, b.T(chatID, "label_pm10")
		g, y = mcfg.PM10Green, mcfg.PM10Yellow
	} else {
		val, prev = m.PM25, m.PM25Prev
		pcent = m.PM25Diff
		icon, label = IconPM25, b.T(chatID, "label_pm25")
		g, y = mcfg.PM25Green, mcfg.PM25Yellow
	}

	getZoneIcon := func(v float64) string {
		if v <= g {
			return IconGreenSq
		}
		if v <= y {
			return IconYellowSq
		}
		return IconRedSq
	}

	formatDiff := func() string {
		if pcent == nil || *pcent == 0 {
			return "    " + IconTrendFlat + " " + b.T(chatID, "msg_no_changes")
		}
		trendIcon := IconTrendUp
		if *pcent < 0 {
			trendIcon = IconTrendDown
		}
		return strings.TrimSuffix(b.T(chatID, "msg_status_diff", map[string]interface {
		}{"trend_icon": trendIcon, "diff_percent": *pcent, "prev": prev, "curr": val}), "\n\n")
	}

	line := b.T(chatID, "msg_status_pm", map[string]interface {
	}{"icon": icon, "label": label, "val": val, "unit": b.T(chatID, "chart_unit_pm"), "zone_icon": getZoneIcon(val)})
	return strings.TrimSpace(line) + "\n" + formatDiff()
}
func (b *Bot) formatPMAlertLine(chatID int64, m *monitor.Measurement, pmType string, mcfg *config.Monitor, winnerID string) string {
	var val, prev, diff float64
	var pcentVal float64
	var icon, label string
	var g, y float64

	if pmType == "PM10" {
		val, prev, diff = m.PM10, m.PM10Prev, m.PM10-m.PM10Prev
		if m.PM10Diff != nil {
			pcentVal = *m.PM10Diff
		}
		icon, label = IconPM10, b.T(chatID, "label_pm10")
		g, y = mcfg.PM10Green, mcfg.PM10Yellow
	} else {
		val, prev, diff = m.PM25, m.PM25Prev, m.PM25-m.PM25Prev
		if m.PM25Diff != nil {
			pcentVal = *m.PM25Diff
		}
		icon, label = IconPM25, b.T(chatID, "label_pm25")
		g, y = mcfg.PM25Green, mcfg.PM25Yellow
	}

	getZoneIcon := func(v float64) string {
		if v <= g {
			return IconGreenSq
		}
		if v <= y {
			return IconYellowSq
		}
		return IconRedSq
	}

	trendIcon := IconTrendUp
	if diff < 0 {
		trendIcon = IconTrendDown
	} else if diff == 0 {
		trendIcon = IconTrendFlat
	}

	var threshold float64
	var thresholdIcon string

	isTransition := false
	if strings.Contains(winnerID, "vals-") {
		isTransition = true
	} else if pmType == "PM10" && strings.Contains(winnerID, "10-") {
		isTransition = true
	} else if pmType == "PM2.5" && strings.Contains(winnerID, "25-") {
		isTransition = true
	}

	if isTransition {

		if (prev <= g && val > g) || (prev > g && val <= g) {
			threshold, thresholdIcon = g, IconGreenSq
		} else {
			threshold, thresholdIcon = y, IconYellowSq
		}
	} else {

		if val <= g {
			threshold, thresholdIcon = g, IconGreenSq
		} else {
			threshold, thresholdIcon = y, IconYellowSq
		}
	}

	unit := b.T(chatID, "chart_unit_pm")
	pcentStr := fmt.Sprintf("%+.2f%%", pcentVal)
	newValStr := fmt.Sprintf("%.2f %s", val, unit)
	if strings.HasPrefix(winnerID, "diff") {
		pcentStr = "<b>" + pcentStr + "</b>"
	} else if strings.HasPrefix(winnerID, "val") {
		newValStr = "<b>" + newValStr + "</b>"
	}

	boundaryLabel := b.T(chatID, "msg_boundary")

	zoneIcon := ""
	if isTransition {
		zoneIcon = getZoneIcon(val) + " "
	}

	return b.T(chatID, "msg_pm_alert_line", map[string]interface{}{
		"icon": icon, "label": label, "diff": diff, "unit": unit, "pcentStr": pcentStr,
		"trendIcon": trendIcon, "prev": prev, "newValStr": newValStr, "zoneIcon": zoneIcon,
		"boundaryLabel": boundaryLabel, "thresholdIcon": thresholdIcon, "threshold": threshold,
	})
}
func (b *Bot) formatWeatherLines(chatID int64, m *monitor.Measurement) string {
	var sb strings.Builder
	if m.Temperature != 0 {
		val := b.convertTemp(m.Temperature, chatID)
		sb.WriteString(strings.TrimSuffix(b.T(chatID, "msg_status_temp", map[string]interface {
		}{"label": b.T(chatID, "msg_temp"), "val": val, "unit": b.unitTempLabel(chatID)}), "\n"))
	}
	if m.Humidity != 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(strings.TrimSuffix(b.T(chatID, "msg_status_hum", map[string]interface {
		}{"label": b.T(chatID, "msg_hum"), "val": m.Humidity}), "\n"))
		if m.Temperature != 0 {
			dp := CalcDewPoint(m.Temperature, m.Humidity)
			dpConverted := b.convertTemp(dp, chatID)
			sb.WriteString("\n")
			sb.WriteString(strings.TrimSuffix(b.T(chatID, "msg_status_dew_point", map[string]interface {
			}{"label": b.T(chatID, "msg_dew_point"), "val": dpConverted, "unit": b.unitTempLabel(chatID)}), "\n"))
		}
	}
	if m.Pressure != 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		val := b.convertPress(m.Pressure, chatID)
		sb.WriteString(strings.TrimSuffix(b.T(chatID, "msg_status_press", map[string]interface {
		}{"label": b.T(chatID, "msg_press"), "val": val, "unit": b.unitPressLabel(chatID)}), "\n"))
	}
	return sb.String()
}
func (b *Bot) formatFooter(chatID int64, m *monitor.Measurement) string {
	return b.formatDeviceID(chatID, m.DeviceID)
}
