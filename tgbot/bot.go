package tgbot

import (
	"context"
	"fmt"
	"html"
	"strings"
	"sync"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/rs/zerolog/log"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/monitor"
)

// chatState tracks what the bot is waiting for from a specific chat.
type chatState int

const (
	stateIdle          chatState = iota
	stateAwaitDeviceID           // waiting for user to type a device ID to subscribe
)

// Button labels for the persistent reply keyboard.
const (
	btnList        = iconList + " Мои подписки"
	btnStatus      = iconStatus + " Статус"
	btnSettings    = iconSettings + " Уведомления"
	btnHistory     = iconHistory + " История"
	btnSubscribe   = iconSubscribe + " Подписаться"
	btnUnsubscribe = iconUnsubscribe + " Отписаться"
	btnMainMenu    = iconBack + " Главное меню"
)

// Icons used throughout the bot UI.
const (
	iconBot         = "🌬️" // Заголовок бота
	iconList        = "📋"  // Списки / Подписки
	iconStatus      = "📊"  // Статус / Данные
	iconSettings    = "🔔"  // Настройки
	iconHistory     = "📈"  // История
	iconSubscribe   = "➕"  // Добавить
	iconUnsubscribe = "❌"  // Удалить / Отписаться
	iconBack        = "🔙"  // Назад
	iconDate        = "📅"  // Дата
	iconTime        = "🕐"  // Время
	iconPM10        = "💨"  // Частицы PM10
	iconPM25        = "░"  // Частицы PM2.5
	iconTemp        = "🌡"  // Температура
	iconHum         = "💧"  // Влажность
	iconPress       = "🗿"  // Давление
	iconAlert       = "🚨"  // Предупреждение
	iconSuccess     = "✅"  // Норма / Успех
	iconInfo        = "ℹ️" // Информация
	iconEmpty       = "📭"  // Пусто
	iconDelete      = "🗑"  // Удаление
	iconDevice      = "📡"  // Устройство / ID
	iconUnknown     = "❓"  // Нет данных
	iconTrendUp     = "🔺"  // Рост (в процентах)
	iconTrendDown   = "▼"  // Снижение (в процентах)
	iconTrendFlat   = "—"  // Без изменений
	iconClock       = "⏱"  // Интервалы времени
	iconBullet      = "•"  // Маркер списка
	iconLoud        = "🔔"  // Со звуком
	iconSilent      = "🔕"  // Без звука
)

// hPa to mmHg conversion factor.
const hPaToMmHg = 0.750064

// Bot wraps the Telegram bot API and the subscription store.
type Bot struct {
	api     *telego.Bot
	handler *th.BotHandler
	store   *Store
	monitor *monitor.MonitorService
	cfg     *config.TgBot

	stateMu sync.Mutex
	states  map[int64]chatState
}

// NewBot creates and starts the Telegram bot using Telego library.
func NewBot(cfg *config.TgBot, ms *monitor.MonitorService) (*Bot, error) {
	var opts []telego.BotOption
	if cfg.Debug {
		opts = append(opts, telego.WithDefaultDebugLogger())
	}

	api, err := telego.NewBot(cfg.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("tgbot: failed to create bot: %w", err)
	}

	// Long polling updates
	updates, err := api.UpdatesViaLongPolling(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("tgbot: failed to start updates: %w", err)
	}

	handler, err := th.NewBotHandler(api, updates)
	if err != nil {
		return nil, fmt.Errorf("tgbot: failed to create handler: %w", err)
	}

	b := &Bot{
		api:     api,
		handler: handler,
		store:   NewStore(cfg.JsonFile),
		monitor: ms,
		cfg:     cfg,
		states:  make(map[int64]chatState),
	}

	b.registerHandlers()
	b.registerCommands()

	self, _ := api.GetMe(context.Background())
	log.Info().Str("username", self.Username).Msg("tgbot: bot authorized (telego)")
	return b, nil
}

func (b *Bot) registerCommands() {
	cmds := []telego.BotCommand{
		{Command: "start", Description: "Запустить бота / справка"},
		{Command: "list", Description: "Мои подписки / Управление"},
		{Command: "status", Description: "Последние данные по устройству"},
		{Command: "help", Description: "Справка"},
	}
	_ = b.api.SetMyCommands(context.Background(), &telego.SetMyCommandsParams{Commands: cmds})
}

func (b *Bot) registerHandlers() {
	// Handlers for Telego v1.x (Handler signature: func(ctx *th.Context, update telego.Update) error)

	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
		b.setState(update.Message.Chat.ID, stateIdle)
		b.sendHelp(update.Message.Chat.ID)
		return nil
	}, th.CommandEqual("start"), th.CommandEqual("help"))

	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
		b.cmdList(update.Message.Chat.ID)
		return nil
	}, th.CommandEqual("list"))

	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
		b.cmdStatusMenu(update.Message.Chat.ID)
		return nil
	}, th.CommandEqual("status"))

	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
		b.cmdUnsubscribeMenu(update.Message.Chat.ID)
		return nil
	}, th.CommandEqual("unsubscribe"))

	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
		text := update.Message.Text
		parts := strings.SplitN(text, " ", 2)
		if len(parts) < 2 {
			b.promptDeviceID(update.Message.Chat.ID)
			return nil
		}
		b.cmdSubscribeDevice(update.Message.Chat.ID, parts[1])
		return nil
	}, th.CommandEqual("subscribe"))

	// Button text and state-based messages
	b.handler.Handle(func(ctx *th.Context, update telego.Update) error {
		b.handleMessage(update.Message)
		return nil
	}, th.AnyMessageWithText())

	// Callbacks
	b.handler.HandleCallbackQuery(func(ctx *th.Context, query telego.CallbackQuery) error {
		b.handleCallback(&query)
		return nil
	}, th.AnyCallbackQuery())
}

func mainKeyboard() *telego.ReplyKeyboardMarkup {
	return &telego.ReplyKeyboardMarkup{
		Keyboard: [][]telego.KeyboardButton{
			{
				tu.KeyboardButton(btnList),
				tu.KeyboardButton(btnStatus),
			},
			{
				tu.KeyboardButton(btnSettings),
				tu.KeyboardButton(btnHistory),
			},
		},
		ResizeKeyboard: true,
		IsPersistent:   true,
	}
}

func subscriptionKeyboard() *telego.ReplyKeyboardMarkup {
	return &telego.ReplyKeyboardMarkup{
		Keyboard: [][]telego.KeyboardButton{
			{
				tu.KeyboardButton(btnSubscribe),
				tu.KeyboardButton(btnUnsubscribe),
			},
			{
				tu.KeyboardButton(btnMainMenu),
			},
		},
		ResizeKeyboard: true,
		IsPersistent:   true,
	}
}

// Run starts the bot update loop (blocking).
func (b *Bot) Run() {
	b.handler.Start()
}

// SendWarning delivers a warning message to all subscribers.
func (b *Bot) SendWarning(deviceID string, m *monitor.Measurement, messages []string, silent bool) {
	chats := b.store.Subscribers(deviceID)
	if len(chats) == 0 {
		return
	}

	text := b.formatWarning(deviceID, m, messages)
	for _, chatID := range chats {
		params := tu.Message(tu.ID(chatID), text).
			WithParseMode(telego.ModeHTML)
		params.DisableNotification = silent
		_, _ = b.api.SendMessage(context.Background(), params)
	}
}

// SendClear delivers a "values returned to normal" notification.
func (b *Bot) SendClear(deviceID string, m *monitor.Measurement, messages []string) {
	chats := b.store.Subscribers(deviceID)
	if len(chats) == 0 {
		return
	}
	text := b.formatClear(deviceID, m, messages)
	for _, chatID := range chats {
		params := tu.Message(tu.ID(chatID), text).
			WithParseMode(telego.ModeHTML)
		_, _ = b.api.SendMessage(context.Background(), params)
	}
}

// ─── state helpers ────────────────────────────────────────────────────────────

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

// ─── message handler ──────────────────────────────────────────────────────────

func (b *Bot) handleMessage(msg *telego.Message) {
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	if text == "" {
		return
	}

	if b.getState(chatID) == stateAwaitDeviceID {
		b.setState(chatID, stateIdle)
		b.cmdSubscribeDevice(chatID, text)
		return
	}

	switch text {
	case btnList:
		b.cmdList(chatID)
	case btnStatus:
		b.cmdStatusMenu(chatID)
	case btnSettings:
		b.cmdSettings(chatID)
	case btnHistory:
		b.cmdHistoryMenu(chatID)
	case btnSubscribe:
		b.promptDeviceID(chatID)
	case btnUnsubscribe:
		b.cmdUnsubscribeMenu(chatID)
	case btnMainMenu:
		b.sendHelp(chatID)
	}
}

// ─── commands ─────────────────────────────────────────────────────────────────

func (b *Bot) sendHelp(chatID int64) {
	text := fmt.Sprintf(`%s <b>AQI Notifier Bot</b>

Этот бот отслеживает качество воздуха по данным ваших датчиков и присылает уведомления о превышении порогов или резких изменениях.

<b>Основные команды:</b>
/list — список ваших подписок и управление ими
/status — текущие показатели по всем вашим устройствам
/history — история последних измерений
/help — эта справка

<b>Управление:</b>
• Нажмите <b>%s Мои подписки</b>, чтобы добавить или удалить устройство.
• В разделе настроек (<b>%s Уведомления</b>) можно посмотреть активные пороги и типы алертов.
• Уведомления о переходе в «красную зону» и обратно приходят со звуком. Остальные (динамические изменения) — беззвучно.`,
		iconBot, iconList, iconSettings)

	params := tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(mainKeyboard())
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) promptDeviceID(chatID int64) {
	b.setState(chatID, stateAwaitDeviceID)
	params := tu.Message(tu.ID(chatID), fmt.Sprintf("%s Введите <b>ID устройства</b> для подписки:", iconDevice)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(&telego.ForceReply{ForceReply: true})
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) cmdSubscribeDevice(chatID int64, deviceID string) {
	var text string
	if b.store.Subscribe(chatID, deviceID) {
		text = fmt.Sprintf("%s Вы подписались на устройство <code>%s</code>", iconSuccess, deviceID)
	} else {
		text = fmt.Sprintf("%s Вы уже подписаны на устройство <code>%s</code>", iconInfo, deviceID)
	}
	b.sendWithKeyboard(chatID, text, true)
}

func (b *Bot) cmdList(chatID int64) {
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		text := fmt.Sprintf("%s У вас нет активных подписок.\nНажмите <b>%s Подписаться</b>, чтобы добавить устройство.", iconEmpty, iconSubscribe)
		params := tu.Message(tu.ID(chatID), text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(subscriptionKeyboard())
		_, _ = b.api.SendMessage(context.Background(), params)
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", iconStatus, id)).WithCallbackData(fmt.Sprintf("status:%s", id)),
		})
	}

	params := tu.Message(tu.ID(chatID), fmt.Sprintf("%s <b>Ваши подписки:</b>\nНажмите на устройство для данных или используйте кнопки внизу для управления.", iconList)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)

	b.sendSubscriptionKeyboard(chatID)
}

func (b *Bot) sendSubscriptionKeyboard(chatID int64) {
	params := tu.Message(tu.ID(chatID), "Управление подписками:").
		WithReplyMarkup(subscriptionKeyboard())
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) cmdUnsubscribeMenu(chatID int64) {
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, fmt.Sprintf("%s У вас нет активных подписок.", iconEmpty), false)
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s  %s", iconUnsubscribe, id)).WithCallbackData(fmt.Sprintf("unsub:%s", id)),
		})
	}

	params := tu.Message(tu.ID(chatID), fmt.Sprintf("%s <b>Выберите устройство для отписки:</b>", iconDelete)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) cmdStatusMenu(chatID int64) {
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, fmt.Sprintf("%s У вас нет активных подписок.", iconEmpty), false)
		return
	}
	if len(devices) == 1 {
		b.sendWithKeyboard(chatID, b.formatDeviceStatus(devices[0]), true)
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", iconStatus, id)).WithCallbackData(fmt.Sprintf("status:%s", id)),
		})
	}

	params := tu.Message(tu.ID(chatID), fmt.Sprintf("%s <b>Выберите устройство:</b>", iconDevice)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}

func (b *Bot) cmdSettings(chatID int64) {
	mcfg := b.monitor.GetMonitorConfig()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s <b>Настройки уведомлений</b>\n\n", iconSettings))

	sb.WriteString(fmt.Sprintf("%s <b>Интервал сравнения:</b> %d сек\n", iconClock, mcfg.DiffTime))
	sb.WriteString(fmt.Sprintf("%s <b>Порог PM10:</b> %.1f мкг/м³ (рост >= %.1f%%)\n", iconPM10, mcfg.PM10Value, mcfg.PM10Diff))
	sb.WriteString(fmt.Sprintf("%s <b>Порог PM2.5:</b> %.1f мкг/м³ (рост >= %.1f%%)\n\n", iconPM25, mcfg.PM25Value, mcfg.PM25Diff))

	type alertInfo struct {
		name   string
		isLoud bool
	}

	typeNames := map[string]alertInfo{
		"val10":           {"Превышение PM10 (красная зона)", true},
		"val25":           {"Превышение PM2.5 (красная зона)", true},
		"vals":            {"Превышение PM10 и PM2.5 (красная зона)", true},
		"diff10_neg_over": {"Возврат PM10 в зелёную зону", true},
		"diff25_neg_over": {"Возврат PM2.5 в зелёную зону", true},
		"diffs_neg_over":  {"Возврат PM10 и PM2.5 в зелёную зону", true},

		"diff10":      {"Рост PM10", false},
		"diff25":      {"Рост PM2.5", false},
		"diffs":       {"Рост PM10 и PM2.5", false},
		"diff10_neg":  {"Снижение PM10", false},
		"diff25_neg":  {"Снижение PM2.5", false},
		"diffs_neg":   {"Снижение PM10 и PM2.5", false},
		"diff10_over": {"Рост PM10 внутри красной зоны", false},
		"diff25_over": {"Рост PM2.5 внутри красной зоны", false},
		"diffs_over":  {"Рост PM10 и PM2.5 внутри красной зоны", false},
	}

	sb.WriteString("<b>Активные уведомления:</b>\n")
	if len(mcfg.Warnings) == 0 {
		sb.WriteString("<i>Все уведомления выключены</i>\n")
	} else {
		for _, w := range mcfg.Warnings {
			info, ok := typeNames[w]
			if !ok {
				sb.WriteString(fmt.Sprintf("%s %s\n", iconBullet, w))
				continue
			}
			icon := iconSilent
			if info.isLoud {
				icon = iconLoud
			}
			sb.WriteString(fmt.Sprintf("%s %s\n", icon, info.name))
		}
	}

	sb.WriteString("\n<b>Доступные типы:</b>\n")
	sb.WriteString(fmt.Sprintf("%s — со звуком, %s — без звука\n\n", iconLoud, iconSilent))

	// Group for display
	allTypes := []string{
		"val10", "val25", "vals", "diff10_neg_over", "diff25_neg_over", "diffs_neg_over",
		"diff10", "diff25", "diffs", "diff10_neg", "diff25_neg", "diffs_neg", "diff10_over", "diff25_over", "diffs_over",
	}

	for _, t := range allTypes {
		info := typeNames[t]
		icon := iconSilent
		if info.isLoud {
			icon = iconLoud
		}
		sb.WriteString(fmt.Sprintf("%s %s\n", icon, info.name))
	}

	b.sendWithKeyboard(chatID, sb.String(), true)
}

func (b *Bot) cmdHistoryMenu(chatID int64) {
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, fmt.Sprintf("%s У вас нет активных подписок.", iconEmpty), false)
		return
	}
	if len(devices) == 1 {
		b.sendWithKeyboard(chatID, b.formatDeviceHistory(devices[0]), true)
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", iconHistory, id)).WithCallbackData(fmt.Sprintf("history:%s", id)),
		})
	}

	params := tu.Message(tu.ID(chatID), fmt.Sprintf("%s <b>Выберите устройство для просмотра истории:</b>", iconDevice)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}

// ─── callback handler ─────────────────────────────────────────────────────────

func (b *Bot) handleCallback(cq *telego.CallbackQuery) {
	_ = b.api.AnswerCallbackQuery(context.Background(), tu.CallbackQuery(cq.ID))

	data := cq.Data
	if cq.Message == nil {
		return
	}
	chatID := cq.Message.GetChat().ID

	switch {
	case strings.HasPrefix(data, "unsub:"):
		deviceID := strings.TrimPrefix(data, "unsub:")
		if b.store.Unsubscribe(chatID, deviceID) {
			params := tu.EditMessageText(tu.ID(chatID), cq.Message.GetMessageID(),
				fmt.Sprintf("%s Вы отписались от устройства <code>%s</code>", iconSuccess, deviceID)).
				WithParseMode(telego.ModeHTML)
			_, _ = b.api.EditMessageText(context.Background(), params)
		}

	case strings.HasPrefix(data, "status:"):
		deviceID := strings.TrimPrefix(data, "status:")
		b.sendWithKeyboard(chatID, b.formatDeviceStatus(deviceID), true)

	case strings.HasPrefix(data, "history:"):
		deviceID := strings.TrimPrefix(data, "history:")
		b.sendWithKeyboard(chatID, b.formatDeviceHistory(deviceID), true)
	}
}

// ─── formatting ───────────────────────────────────────────────────────────────

func (b *Bot) formatDeviceStatus(deviceID string) string {
	m := b.monitor.LastMeasurement(deviceID)
	if m == nil {
		return fmt.Sprintf("%s Нет данных для устройства <code>%s</code>", iconUnknown, deviceID)
	}
	return b.formatMeasurement(deviceID, m)
}

func (b *Bot) formatDeviceHistory(deviceID string) string {
	hist := b.monitor.GetHistory(deviceID)
	if len(hist) == 0 {
		return fmt.Sprintf("%s История для устройства <code>%s</code> пуста.", iconUnknown, deviceID)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s <b>История:</b> <code>%s</code>\n\n", iconHistory, deviceID))

	for i := len(hist) - 1; i >= 0; i-- {
		m := hist[i]
		t := m.Timestamp.Local()

		sb.WriteString(fmt.Sprintf("%s <b>%02d:%02d:%02d</b>", iconBullet, t.Hour(), t.Minute(), t.Second()))
		if m.PM10Diff != nil || m.PM25Diff != nil {
			sb.WriteString(" ")
			var trends []string
			if m.PM10Diff != nil {
				trends = append(trends, formatTrendIcon(*m.PM10Diff))
			}
			if m.PM25Diff != nil {
				trends = append(trends, formatTrendIcon(*m.PM25Diff))
			}
			sb.WriteString(strings.Join(trends, " / "))
		}
		sb.WriteString("\n")

		sb.WriteString(fmt.Sprintf("  %s PM10: <b>%.1f</b>, %s PM2.5: <b>%.1f</b>\n", iconPM10, m.PM10, iconPM25, m.PM25))

		if m.Temperature != 0 || m.Humidity != 0 || m.Pressure != 0 {
			var parts []string
			if m.Temperature != 0 {
				parts = append(parts, fmt.Sprintf("%s %.1f°C", iconTemp, m.Temperature))
			}
			if m.Humidity != 0 {
				parts = append(parts, fmt.Sprintf("%s %.0f%%", iconHum, m.Humidity))
			}
			if m.Pressure != 0 {
				parts = append(parts, fmt.Sprintf("%s %.0fмм", iconPress, m.Pressure*hPaToMmHg))
			}
			sb.WriteString("  " + strings.Join(parts, ", ") + "\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func formatTrendIcon(pct float64) string {
	if pct > 0 {
		return fmt.Sprintf("%s+%.1f%%", iconTrendUp, pct)
	}
	if pct < 0 {
		return fmt.Sprintf("%s%.1f%%", iconTrendDown, pct)
	}
	return iconTrendFlat
}

func diffTag(pct float64, prev float64) string {
	if pct > 0 {
		return fmt.Sprintf("\n    %s<b>+%.1f%%</b> <i>(было %.2f)</i>", iconTrendUp, pct, prev)
	}
	if pct < 0 {
		return fmt.Sprintf("\n    %s<b>%.1f%%</b> <i>(было %.2f)</i>", iconTrendDown, pct, prev)
	}
	return " —"
}

func (b *Bot) formatMeasurement(deviceID string, m *monitor.Measurement) string {
	var sb strings.Builder

	t := m.Timestamp.Local()
	sb.WriteString(fmt.Sprintf("%s %s %s %s\n\n",
		iconDate, t.Format("02.01.2006"),
		iconTime, t.Format("15:04:05"),
	))

	sb.WriteString(fmt.Sprintf("%s <b>PM10:</b> %.2f мкг/м³", iconPM10, m.PM10))
	if m.PM10Diff != nil {
		sb.WriteString(diffTag(*m.PM10Diff, m.PM10Prev))
	}
	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("%s <b>PM2.5:</b> %.2f мкг/м³", iconPM25, m.PM25))
	if m.PM25Diff != nil {
		sb.WriteString(diffTag(*m.PM25Diff, m.PM25Prev))
	}
	sb.WriteString("\n")

	if m.Temperature != 0 {
		sb.WriteString(fmt.Sprintf("%s <b>Температура:</b> %.1f °C\n", iconTemp, m.Temperature))
	}
	if m.Humidity != 0 {
		sb.WriteString(fmt.Sprintf("%s <b>Влажность:</b> %.1f%%\n", iconHum, m.Humidity))
	}
	if m.Pressure != 0 {
		mmhg := m.Pressure * hPaToMmHg
		sb.WriteString(fmt.Sprintf("%s <b>Давление:</b> %.1f мм.рт.ст.\n", iconPress, mmhg))
	}

	sb.WriteString(fmt.Sprintf("\n%s <b>Устройство:</b> <code>%s</code>\n", iconDevice, deviceID))

	return sb.String()
}

func (b *Bot) formatWarning(deviceID string, m *monitor.Measurement, messages []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s <b>ПРЕДУПРЕЖДЕНИЕ</b>\n\n", iconAlert))
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("%s %s\n", iconBullet, html.EscapeString(msg)))
	}
	sb.WriteString("\n")
	sb.WriteString(b.formatMeasurement(deviceID, m))
	return sb.String()
}

func (b *Bot) formatClear(deviceID string, m *monitor.Measurement, messages []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s <b>НОРМА ВОССТАНОВЛЕНА</b>\n\n", iconSuccess))
	for _, msg := range messages {
		sb.WriteString(fmt.Sprintf("%s %s\n", iconBullet, html.EscapeString(msg)))
	}
	sb.WriteString("\n")
	sb.WriteString(b.formatMeasurement(deviceID, m))
	return sb.String()
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (b *Bot) sendWithKeyboard(chatID int64, text string, html bool) {
	params := tu.Message(tu.ID(chatID), text).
		WithReplyMarkup(mainKeyboard())
	if html {
		params.WithParseMode(telego.ModeHTML)
	}
	_, _ = b.api.SendMessage(context.Background(), params)
}
