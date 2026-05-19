# AQI Notifier Localization Documentation

> 🇷🇺 [Русская версия](readme.ru.md)

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

### 1.3. The `%raw` Modifier
When a placeholder's value is already a ready-made HTML string or pre-rendered text that must not be escaped or looked up in the dictionary, use the `%raw` modifier:
`"{correctionsList%raw}"` — inserts the variable value as-is, without further processing.

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

### 2.3. Conditional Block (block-if)
Syntax: `{?boolVar%content_if_true%}`
The false branch can be omitted if only the true branch is needed.
**Example:**
`"{?hasCorrections%Value corrections:\n{correctionsList%raw}\n\n%}"` — the block is rendered only when corrections exist.

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

## 4. Placeholder Reference (Go → JSON)

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
- `{aqiLevel}`: Level number (1–7).
- `{aqiIcon}`: Colored level icon (🟢, 🟡, 🟠, 🔴, 🟣, 🟤, ⚫).
- `{aqiName}`: Level name ("Good", "Moderate", etc.).
- `{aqiStandardFlag}`: Selected standard flag (🇪🇺, 🇺🇸, 🇨🇳).

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

### 4.6. Threshold Settings (`msgAqiCycleMenu`, `msgThresholdsMenu`, `msgResetConfirm`)
- `{l1_25}`, `{l2_25}`: Level 1 and 2 thresholds for PM2.5.
- `{dyn25}`: Dynamics threshold (%) for PM2.5.
- `{l1_10}`, `{l2_10}`: Level 1 and 2 thresholds for PM10.
- `{dyn10}`: Dynamics threshold (%) for PM10.

### 4.7. Device Settings (`msgDeviceSettings`, `msgDeviceSettingsCorrItem`)
- `{type}`: Human-readable sensor type name (e.g., "ArmAQI (SDS011)").
- `{hasCorrections}`: `true` if correction formulas are defined for the device type.
- `{correctionsList%raw}`: Pre-rendered HTML list of correction rows (inserted as `%raw`).

For a single correction row template (`msgDeviceSettingsCorrItem`):
- `{key}`: Human-readable field label (taken from the `labelField_<field>` key).
- `{formula}`: Already-formatted formula string.

For the ternary formula template (`msgFormulaTernary`):
- `{cond%raw}`: Ternary operator condition.
- `{trueVal%raw}`: Value when condition is true.
- `{falseVal%raw}`: Value when condition is false.

---

## 5. Resource Files (`res/`)

Files in the `res/` directory define the visual style and AQI calculation logic.

### 5.1. Colors (`colors.json`)
Centralized color code reference.
- **Purpose**: Used for chart rendering and as color placeholder values.
- **Usage**: In `aqi.json` or other resources, colors can be referenced via `{colorName}`. This allows changing the entire application palette in one file.

### 5.2. Icons (`ico.json`)
Global dictionary of emoji and graphic symbols.
- **Purpose**: Separation of graphics from text. All icons are available in templates as `{icoName}` placeholders.
- **Flexibility**: Allows quickly changing the visual style without modifying logic or text templates.

### 5.3. AQI Standards (`aqi.json`)
Defines the index calculation rules and zone boundaries for various regions (US, EU, CN). It is the "single source of truth" for the AQI mathematical model.
- **Main fields**:
    - `tag`: Standard code (`Us`, `Eu`, `Cn`).
    - `indexPoints`: Index scale points (IAQI).
    - `breakpoints25` / `breakpoints10`: PM concentration threshold values.
    - `zones`: Level descriptions (level number, name, color, icon).

### 5.4. Override Mechanism
The bot supports dynamic overriding of `aqi.json` data through localization and icon files. This allows adapting a standard to different languages (e.g., grammatical cases for zone names). Lookup order:
1. **Flags**: Key `icoFlag<TAG>` in `ico.json`.
2. **Level icons**: Key `icoAqi<TAG>Level<N>` in `ico.json`.
3. **Level names**: Key `aqiNameL<N><Tag>` in the dictionary (`en.json`).
4. **Standard names**: Keys `standard<Tag>` (full) and `txtStandard<Tag>` (short).

*If a specific key is not found in the localization, values from `aqi.json` defaults are used.*

---

## 6. Development Rules

1. **No Logic in Go**: It is forbidden to build lists or choose icons in code. Use `evt*` flags and conditionals in JSON.
2. **Naming**: Always use `camelCase` for keys and variables.
3. **HTML**: Use native tags like `<b>`, `<i>`, `<code>`.
4. **Unicode**: Prohibit Unicode escaping (`\u003c`) in JSON files.
5. **Sorting**: JSON keys must be sorted topically.
6. **`labelField_<field>`**: For any new measurable field (pm01, co2, etc.), a corresponding `labelField_<field>` key must be added to every language file.
