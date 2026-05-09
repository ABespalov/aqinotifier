# Localization and Placeholder System Documentation

The system is based on recursive resolution of placeholders using the format `@key%format@`.

---

## 1. Atomic Placeholders (Data from Code)
These are primary values provided by the bot's core logic. They do not depend on other placeholders and serve as the "building blocks" for all messages.

| Placeholder | Type | Description |
| :--- | :--- | :--- |
| `@val@` | Float | Current sensor value (number). |
| `@curr@` / `@prev@`| Float | Current and previous values (used for comparisons and deltas). |
| `@diff@` | Float | Absolute difference between current and previous values. |
| `@diff_percent@`| Float | Percentage change (number). |
| `@aqi_val@` | Float | Calculated AQI index. |
| `@device_id@` | String | Unique device identifier (e.g., `12345`). |
| `@name@` | String | User-defined device name or event name. |
| `@date@` | Time | Time object (date part). Requires a format or the `format_date` key. |
| `@time@` | Time | Time object (time part). Requires a format or the `format_time` key. |
| `@unit@` | String | Unit of measurement (`°C`, `µg/m³`, etc.). |
| `@err@` | String | System error message text. |
| `@count@` | Int | Number of records in the history. |
| `@status@` | String | State toggle (`enabled`/`disabled`). |
| `@icon@` | String | Primary icon. Can take values `IconPM25`, `IconPM10`, or AQI level circles (`IconGreen`, `IconRed`, etc.). |
| `@zone_icon@` | String | Pollution zone icon. Takes `IconGreenSq`, `IconYellowSq`, or `IconRedSq`. |
| `@trend_icon@`| String | Change direction icon. Takes `IconTrendUp`, `IconTrendDown`, or `IconTrendFlat`. |

**Specific Pre-formatted Strings:**
*   `@pcentStr@` — Formatted percentage string with sign and HTML tags (e.g., `<b>+15.2%</b>`).
*   `@newValStr@` — Formatted value string with unit (e.g., `<b>25.4 µg/m³</b>`).
*   `@aqi_standard_flag@` — Flag placeholder (usually resolves to `@flag_us@` or `@flag_eu@`).

---

## 2. Meta-holders (JSON Structure)
Meta-holders are the keys defined in localization files. They use atomic placeholders to form human-readable messages.

### 2.1. Constants and Icons (Base Meta-holders)
Simple strings or references to icons that don't depend on dynamic data or only depend on icons.
- **Icons**: Any key from `ico.json` is available directly as `@IconName@`.
- **@icon@ (placeholder)**: This is a dynamic "container". The code determines which specific icon from `ico.json` to insert into it (e.g., `@IconGreen@` or `@IconRed@`).
- **Flags**: `flag_us` (`@IconFlagUS@`), `flag_eu` (`@IconFlagEU@`).
- **Labels**: `label_pm25`, `label_dynamics`, `label_threshold`.

### 2.2. Message Templates and Dependencies
The following table lists JSON key groups and the atomic placeholders available within them.

| Key Group (JSON) | Available Atomic Placeholders | Description |
| :--- | :--- | :--- |
| `msg_status_header` | `@date@`, `@time@` | Status message header (data reception date and time). |
| `msg_status_aqi` | `@icon@`, `@aqi_val@`, `@aqi_name@`, `@aqi_standard_flag@` | Main AQI index line in the status message. |
| `msg_status_pm` | `@icon@`, `@label@`, `@val@`, `@unit@`, `@zone_icon@` | Main row for PM2.5 or PM10 indicators. |
| `msg_status_diff` | `@trend_icon@`, `@diff_percent@`, `@prev@`, `@curr@` | Sub-line showing dynamics (percentage, trend, sign). |
| `msg_status_temp/hum/press`| `@label@`, `@val@`, `@unit@` | Meteorological data rows (temperature, humidity, pressure). |
| `alert_aqi_clean/full` | `@icon@`, `@standard@`, `@aqi_standard_flag@`, `@aqi_val@`, `@aqi_name@` | Push-notification text for AQI level changes. |
| `alert_pm_rise/fall_to/in` | `@pm@`, `@zone@`, `@zone_acc@`, `@zone_pre@` | Push-notification text for PM pollution zone transitions. |
| `msg_history_footer` | `@count@`, `@device_id@` | Information footer in the measurement history message. |
| `msg_reset_confirm_details`| `@pm25_g@`, `@pm25_y@`, `@pm25_dyn@`, `@pm10_g@...`, `@std_name@`, `@aqi_standard_flag@` | Detailed list of settings to be reset. |
| `msg_threshold_upd` | `@title@`, `@old@`, `@new@` | Confirmation of a successful setting or threshold update. |

> [!NOTE]
> Atomic placeholders are context-bound. For example, `@aqi_val@` is only available in messages related to AQI status or alerts. If used in an unsupported context, it will remain unresolved.

---

## 3. Mapping Atomic Placeholders to Icons
This table specifies which icons from `ico.json` the bot inserts into certain atomic placeholders based on the logic.

| Placeholder | Context (JSON Key) | Icons used from `ico.json` |
| :--- | :--- | :--- |
| `@icon@` | `msg_status_aqi` | **Level circles**: `IconGreen`, `IconYellow`, `IconOrange`, `IconRed`, `IconPurple`, `IconMaroon`, `IconBlack`, `IconBlue`. |
| `@icon@` | `msg_status_pm` | **Particle type**: `IconPM25` or `IconPM10`. |
| `@zone_icon@` | `msg_status_pm`, `msg_threshold_title_fmt` | **Zone squares**: `IconGreenSq`, `IconYellowSq`, `IconRedSq`. |
| `@trend_icon@` | `msg_status_diff`, `msg_pm_alert_line` | **Trends**: `IconTrendUp`, `IconTrendDown`, `IconTrendFlat`. |
| `@thresholdIcon@`| `msg_pm_alert_line` | **Zone squares**: `IconGreenSq`, `IconYellowSq`. |

---

## 4. Date and Time Formatting

The system supports standard Go time formatting specifiers for `@date@` and `@time@` objects.

| Format | Result | Description |
| :--- | :--- | :--- |
| `%02.01.2006` | `10.05.2026` | Date: Day.Month.Year |
| `%15:04:05` | `14:30:05` | Time: 24-hour format |
| `%03:04 PM` | `02:30 PM` | Time: 12-hour format |
| `%Mon, Jan 02` | `Sun, May 10` | Weekday and short date |

> [!TIP]
> If a template uses `@date@` without a `%` specifier, the bot will look for the `format_date` key in the JSON dictionary and apply its value as the format. This also applies to `format_time` and `format_datetime`.

---

## 4. Recursive Resolution (Example)

Resolution chain: **Atomic Argument -> Meta-holder (JSON) -> Icon (ico.json)**.

1. Code calls `T("btn_aqi_standard", {"std": "US", "aqi_standard_flag": "@flag_us@"})`.
2. In JSON: `"btn_aqi_standard": "Standard: @std@ @aqi_standard_flag@"`
3. Resolving `@std@` -> `Standard: US @aqi_standard_flag@`
4. Resolving `@aqi_standard_flag@` (passed as an argument) -> `Standard: US @flag_us@`
5. In JSON: `"flag_us": "@IconFlagUS@"` -> `Standard: US @IconFlagUS@`
6. In `ico.json`: `"IconFlagUS": "🇺🇸"` -> **`Standard: US 🇺🇸`**
