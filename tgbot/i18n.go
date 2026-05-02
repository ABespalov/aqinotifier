package tgbot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

var (
	i18nBundle *i18n.Bundle
)

func init() {
	// Initialize bundle with English as default
	i18nBundle = i18n.NewBundle(language.English)
	i18nBundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	// Register hardcoded English as a baseline fallback
	enData, _ := json.Marshal(fallbackEN)
	i18nBundle.MustParseMessageFileBytes(enData, "en.json")

	// Try to load external files from "lng" directory if it exists
	loadExternalTranslations("lng")
}

// loadExternalTranslations walks the given directory and loads all JSON translation files into the bundle.
func loadExternalTranslations(dir string) {
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && filepath.Ext(path) == ".json" {
			_, loadErr := i18nBundle.LoadMessageFile(path)
			if loadErr != nil {
				fmt.Printf("i18n: failed to load %s: %v\n", path, loadErr)
			}
		}
		return nil
	})
}

// T returns a localized string for the given key and chat ID, using the user's preferred language.
// It supports positional arguments for formatting (fmt.Sprintf style).
func (b *Bot) T(chatID int64, key string, args ...interface{}) string {
	lang := b.store.GetLanguage(chatID)
	return b.TLang(lang, key, args...)
}

// detectLang maps a Telegram language code to a supported application language.
func (b *Bot) detectLang(langCode string) string {
	if strings.HasPrefix(langCode, "ru") {
		return "ru"
	}
	return "en"
}

// TLang returns a localized string for a specific language code.
func (b *Bot) TLang(lang, key string, args ...interface{}) string {
	if lang == "" {
		lang = "en"
	}

	localizer := i18n.NewLocalizer(i18nBundle, lang, "en")
	msg, err := localizer.Localize(&i18n.LocalizeConfig{
		MessageID: key,
	})

	if err != nil {
		if val, ok := fallbackEN[key]; ok {
			msg = val
		} else {
			return fmt.Sprintf("!!%s!!", key)
		}
	}

	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// fallbackEN contains the hardcoded English localization as a final safety net.
var fallbackEN = map[string]string{
	"btn_list":            "%s My Subscriptions",
	"btn_status":          "%s Status",
	"btn_settings":        "%s Settings",
	"btn_history":         "%s History",
	"btn_subscribe":       "%s Subscribe",
	"btn_unsubscribe":     "%s Unsubscribe",
	"btn_main_menu":       "%s Main Menu",
	"btn_info":            "%s Info",
	"btn_interval":        "%s Interval",
	"btn_thresholds":      "%s Thresholds",
	"btn_sound_profiles":  "%s With Sound",
	"btn_silent_profiles": "%s Silent",
	"btn_pm10_threshold":  "%s PM10 Threshold",
	"btn_pm25_threshold":  "%s PM2.5 Threshold",
	"cmd_start_desc":      "Start the bot / Help",
	"cmd_list_desc":       "My subscriptions / Management",
	"cmd_status_desc":     "Current device status",
	"cmd_help_desc":       "Help information",

	"msg_help":            "%s <b>AQI Notifier Bot</b>\n\nThis bot monitors air quality data from your sensors and sends notifications about threshold exceedances or sharp changes.\n\n<b>Main Commands:</b>\n/list — your subscriptions and management\n/status — current data for all your devices\n/history — history of recent measurements\n/help — this help message\n\n<b>Management:</b>\n• Press <b>%s My Subscriptions</b> to add or remove a device.\n• In the settings section (<b>%s Settings</b>), you can change thresholds, intervals, and notification types.\n• Notifications for entering the \"red zone\" and back are sent with sound. Others (dynamic changes) are silent.",
	"msg_prompt_device":   "%s Enter <b>Device ID</b> to subscribe:",
	"msg_subscribed":      "%s You subscribed to device <code>%s</code>",
	"msg_already_sub":     "%s You are already subscribed to device <code>%s</code>",
	"msg_no_subs":         "%s You have no active subscriptions.\nPress <b>%s Subscribe</b> to add a device.",
	"msg_your_subs":       "%s <b>Your subscriptions:</b>\nClick on a device for data or use the buttons below for management.",
	"msg_manage_subs":     "Subscription Management:",
	"msg_select_unsub":    "%s <b>Select device to unsubscribe:</b>",
	"msg_select_device":   "%s <b>Select device:</b>",
	"msg_settings_title":  "%s <b>Monitoring Settings</b>\n\nSelect a section to configure personal notifications.",
	"msg_info_title":      "%s <b>System Information</b>\n\n",
	"msg_app_version":     "App Version: <b>%s</b>\n",
	"msg_bot_version":     "Bot Version: <b>%s</b>\n\n",
	"msg_mon_settings":    "%s <b>Monitoring Settings:</b>\n",
	"msg_interval_info":   "• Update interval: <b>%d</b> sec.\n",
	"msg_pm10_info":       "• PM10 Threshold: <b>%.1f</b> µg/m³ (growth >= <b>%.1f%%</b>)\n",
	"msg_pm25_info":       "• PM2.5 Threshold: <b>%.1f</b> µg/m³ (growth >= <b>%.1f%%</b>)\n\n",
	"msg_loud_alerts":     "%s <b>Loud Notifications:</b>\n",
	"msg_silent_alerts":   "%s <b>Silent Notifications:</b>\n",
	"msg_thresholds_menu": "%s <b>Pollution Thresholds</b>\n\n%s PM10 Threshold: <b>%.1f</b>\n%s PM2.5 Threshold: <b>%.1f</b>\n\nSelect a threshold to change.",
	"msg_interval_prompt": "%s <b>Comparison Interval</b>\n\nCurrent interval: <b>%d</b> sec.\n\nEnter new value in seconds (e.g., 3600 for an hour):",
	"msg_threshold_prompt": "%s <b>Changing %s threshold</b>\n\nEnter new numeric value:",
	"msg_error_number":    "%s Error: enter a numeric value.",
	"msg_threshold_upd":   "%s %s threshold updated to <b>%.1f</b>",
	"msg_error_positive":  "%s Error: enter a positive integer.",
	"msg_interval_upd":    "%s Interval updated to <b>%d</b> sec.",
	"msg_select_history":  "%s <b>Select device to view history:</b>",
	"msg_sound_settings":  "Click on a notification type to enable or disable it:\n\n",
	"msg_toggle_success":  "%s %s is now <b>%s</b>",
	"msg_enabled":         "enabled",
	"msg_disabled":        "disabled",
	"msg_unsubscribed":    "%s You unsubscribed from device <code>%s</code>",
	"msg_norma":           "NORMAL RECOVERED",
	"msg_alert":           "WARNING",
	"msg_decrease":        "LEVEL DECREASE",
	"msg_was":             "was",
	"msg_temp":            "Temperature",
	"msg_hum":             "Humidity",
	"msg_press":           "Pressure",
	"msg_unit_mmhg":       "mmHg",
	"msg_device":          "Device",
	"msg_history_empty":   "%s History for device <code>%s</code> is empty.",
	"msg_history_title":   "%s <b>History:</b> <code>%s</code>",
	"msg_error_charts":    "%s Error generating charts: %v",
	"msg_error_send_ch":   "%s Failed to send charts.",
	"msg_invalid_device_id": "%s Device ID must contain only digits.",
	"status_no_data":      "%s No data for device <code>%s</code>",
	"chart_pm_title":      "PM10 & PM2.5",
	"chart_unit_pm":       "µg/m³",

	"alert_val10":           "PM10 Threshold",
	"alert_val25":           "PM2.5 Threshold",
	"alert_vals":            "PM10 + PM2.5 Threshold",
	"alert_diff10_neg_over": "PM10 back to normal",
	"alert_diff25_neg_over": "PM2.5 back to normal",
	"alert_diffs_neg_over":  "Both back to normal",
	"alert_diff10":          "PM10 growth",
	"alert_diff25":          "PM2.5 growth",
	"alert_diffs":           "General growth",
	"alert_diff10_neg":      "PM10 decrease",
	"alert_diff25_neg":      "PM2.5 decrease",
	"alert_diffs_neg":       "General decrease",

	"alert_val10_exceeded":  "PM10 exceeded threshold: %.2f >= %.2f",
	"alert_val10_normal":    "PM10 (%.2f) back to normal (< %.2f)",
	"alert_val25_exceeded":  "PM2.5 exceeded threshold: %.2f >= %.2f",
	"alert_val25_normal":    "PM2.5 (%.2f) back to normal (< %.2f)",
	"alert_vals_exceeded":   "PM10 (%.2f) and PM2.5 (%.2f) exceeded thresholds",
	"alert_vals_normal":     "PM10 (%.2f) and PM2.5 (%.2f) back to normal",
	"alert_diff10_growth":   "Sharp PM10 growth: %.1f%% >= %.1f%% in %ds (was: %.2f, now: %.2f)",
	"alert_diff25_growth":   "Sharp PM2.5 growth: %.1f%% >= %.1f%% in %ds (was: %.2f, now: %.2f)",
	"alert_diffs_growth":    "Sharp growth of PM10 (%.1f%%) and PM2.5 (%.1f%%) in %ds",
	"alert_diff10_decrease": "Sharp PM10 decrease: %.1f%% in %ds (was: %.2f, now: %.2f)",
	"alert_diff25_decrease": "Sharp PM2.5 decrease: %.1f%% in %ds (was: %.2f, now: %.2f)",
	"alert_diffs_decrease":  "Sharp decrease of PM10 (%.1f%%) and PM2.5 (%.1f%%) in %ds",
	"alert_diff10_crit":     "Critical PM10 growth in pollution zone: %.1f%% in %ds (now %.2f)",
	"alert_diff25_crit":     "Critical PM2.5 growth in pollution zone: %.1f%% in %ds (now %.2f)",
	"alert_diffs_crit":      "Critical growth of indicators in pollution zone (PM10: %.1f%%, PM2.5: %.1f%%)",
	"alert_diff10_clean":    "Sharp PM10 decrease into clean zone: %.1f%% (now %.2f)",
	"alert_diff25_clean":    "Sharp PM2.5 decrease into clean zone: %.1f%% (now %.2f)",
	"alert_diffs_clean":     "Sharp decrease of indicators into clean zone (PM10: %.1f%%, PM2.5: %.1f%%)",
}
