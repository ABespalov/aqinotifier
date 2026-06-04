package tgbot

import "github.com/ABespalov/aqinotifier/config"

// RequestContext encapsulates state for a single Telegram update,
// avoiding repeated lookups of user settings and language.
type RequestContext struct {
	ChatID   int64
	Language string
	Settings *config.Monitor
	Bot      *Bot
}

// NewContext creates a context for the current request, loading settings once.
func (b *Bot) NewContext(chatID int64) *RequestContext {
	return &RequestContext{
		ChatID:   chatID,
		Language: b.store.GetLanguage(chatID),
		Settings: b.store.GetSettings(chatID, b.defaults),
		Bot:      b,
	}
}

// T is a localization wrapper that uses the cached language.
func (ctx *RequestContext) T(key string, args ...interface{}) string {
	return ctx.Bot.TLang(ctx.Language, key, args...)
}

// TDevice is a localization wrapper specific to a device context.
func (ctx *RequestContext) TDevice(key string, deviceID string, args ...map[string]interface{}) string {
	m := make(map[string]interface{})
	if len(args) > 0 {
		for k, v := range args[0] {
			m[k] = v
		}
	}
	m["deviceId"] = deviceID
	m["deviceName"] = ""
	if name, ok := ctx.Settings.DeviceNames[deviceID]; ok {
		m["deviceName"] = name
	}
	return ctx.T(key, m)
}

func (ctx *RequestContext) I(key string) string {
	return ctx.Bot.I(key)
}

func (ctx *RequestContext) C(key string) string {
	return ctx.Bot.C(key)
}

func (ctx *RequestContext) Resolve(key string) string {
	return ctx.Bot.Resolve(key)
}
