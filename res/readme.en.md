# AQI Notifier Localization Documentation (v2.5)

The bot's localization system is based on a **recursive block template engine** with logic support. This architecture completely decouples the UI representation (icons, texts, message structures) from the Go source code.

---

## 1. Basic Syntax

### 1.1. Placeholders
Format: `{key%format}`
- `key`: Key name in JSON or variable name from Go.
- `format`: Optional modifier (formatting for numbers, dates, or case).

### 1.2. Recursive Resolution
If a key value contains other placeholders, they are resolved recursively.
**Example:**
`"txtDevice": "{icoDevice} {deviceName}"`
The engine first finds `icoDevice` in the dictionary, then resolves the `deviceName` variable.

---

## 2. Conditionals (Logic)

Syntax: `{?condition%true_text%false_text}`

### 2.1. Comparison Operators
| Operator | Aliases | Description |
| :--- | :--- | :--- |
| `==` | `eq` | Equals |
| `!=` | `ne` | Not equals |
| `>` | `gt` | Greater than |
| `<` | `lt` | Less than |
| `>=` | `ge` | Greater or equal |
| `<=` | `le` | Less or equal |
| `isEmpty` | - | True if value is empty or 0 |
| `isNotEmpty` | - | True if value is not empty |

### 2.2. Nested Conditionals (Switch)
If `false_text` starts with `?`, it's processed as a next condition level.
**Example:**
`"txtHeader": "{?isAlert%{icoAlert} WARNING%{?isNorma%{icoBackToNorm} NORMAL%{icoInfo} INFO}}"`

---

## 3. Formatting Modifiers

### 3.1. Case Formatters
- `%toUpper`: CONVERTS TO UPPER CASE
- `%toLower`: converts to lower case
- `%toTitle`: Capitalizes Each Word

### 3.2. Numbers (printf)
Uses standard Go syntax:
- `%.1f`: 12.3
- `%.2f`: 12.34
- `%+0.1f`: +12.3 (forced sign)
- `%d`: 123

### 3.3. Date and Time
Time is automatically formatted in the server's **local timezone**.
- `%02.01.2006`: 02.05.2024
- `%15:04:05`: 14:30:05

---

## 4. Placeholder Reference (Go -> JSON)

### 4.1. General Variables (available in `msgStatus`, `msgAlertNotify`, etc.)
- `{date}`, `{time}`: Measurement time object (always local time).
- `{deviceId}`: Technical device ID.
- `{deviceName}`: Device name (user-defined or "Device <ID>").

### 4.2. PM2.5 and PM10 Indicators
- `{val25}`, `{curr25}`: Current PM2.5 value.
- `{prev25}`: Previous PM2.5 value (before change).
- `{diff25}`: Absolute difference (`curr - prev`).
- `{diff25Percent}`: Percentage change.
- `{trend25Icon}`: Trend icon (📈, 📉, ➖).
- `{zone25Icon}`: Pollution zone icon (🟩, 🟨, 🟥).
- `{labelPm25}`: Localized label ("PM2.5").
- `{unitPm}`: Measurement unit ("µg/m³").
- *(Same for PM10: `{val10}`, `{curr10}`, `{prev10}`, `{diff10}`, `{diff10Percent}`, `{trend10Icon}`, `{zone10Icon}`, `{labelPm10}`)*

### 4.3. AQI (Air Quality Index)
- `{aqiVal}`: Numeric index value.
- `{aqiLevel}`: Level number (1-7).
- `{aqiIcon}`: Colored level icon (🟢, 🟡, 🟠, 🔴, 🟣, 🟤, ⚫).
- `{aqiName}`: Level name ("Good", "Moderate", etc.).
- `{aqiStandardFlag}`: Selected standard flag (🇪🇺 or 🇺🇸).

### 4.4. Meteorological Data
- `{valT}`: Temperature (in °C or °F).
- `{labelT}`: Label ("Temperature").
- `{unitT}`: Unit ("°C" or "°F").
- `{valH}`: Humidity (%).
- `{labelH}`: Label ("Humidity").
- `{valDp}`: Calculated dew point.
- `{labelDp}`: Label ("Dew point").
- `{valP}`: Pressure (in mmHg or hPa).
- `{labelP}`: Label ("Pressure").
- `{unitP}`: Unit ("mmHg" or "hPa").

### 4.5. State Variables (Alerts only)
- `{isAlert}`: `true` if at least one worsening event occurred.
- `{isNorma}`: `true` if air quality returned to normal.
- `{isSilent}`: `true` if the notification is silent.
- `{isRise}`: `true` if levels (AQI or PM) increased.
- `{isFall}`: `true` if levels decreased.
- `{isReturn}`: `true` if returned to a clean zone.
- `{isSharp}`: `true` if a sharp dynamics trigger was tripped.
- `{isAqi}`, `{isPm25}`, `{isPm10}`, `{isBoth}`: Event type indicators.
- `{isRed}`, `{isYellow}`, `{isGreen}`: Approximate zone of the event (L3, L2, L1).
- `{evt<EventID>}`: Dynamic flags for all triggered events (e.g., `{evtAqiL2}`, `{evtVal25L3u}`, `{evtDiff10Rise}`).

### 4.6. Threshold Settings (msgAqiCycleMenu, msgThresholdsMenu, msgResetConfirm)
- `{l1_25}`, `{l2_25}`: Level 1 and 2 thresholds for PM2.5.
- `{dyn25}`: Dynamics threshold (%) for PM2.5.
- `{l1_10}`, `{l2_10}`: Level 1 and 2 thresholds for PM10.
- `{dyn10}`: Dynamics threshold (%) for PM10.

---

## 5. Development Rules

1. **No Logic in Go**: It is forbidden to build lists or choose icons in code. Use `evt*` flags and conditionals in JSON.
2. **Naming**: Always use `camelCase` for keys and variables.
3. **HTML**: Use native tags like `<b>`, `<i>`, `<code>`.
4. **Unicode**: Prohibit Unicode escaping (`\u003c`) in JSON files.
5. **Sorting**: JSON keys must be sorted topically.
