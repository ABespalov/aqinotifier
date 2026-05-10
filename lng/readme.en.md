# AQI Notifier Localization Documentation (v2.2)

The bot's localization system is based on a **recursive block template engine** with logic support. This architecture completely decouples the UI representation (icons, texts, message structures) from the Go source code.

---

## 1. Basic Syntax

### 1.1. Placeholders
Format: `@key%format@`
- `key`: Key name in JSON or variable name from Go.
- `format`: Optional modifier (formatting for numbers, dates, or case).

### 1.2. Recursive Resolution
If a key value contains other placeholders, they are resolved recursively.
**Example:**
`"txtDevice": "@icoDevice@ @deviceName@"`
The engine first finds `icoDevice` in the dictionary, then resolves the `deviceName` variable.

---

## 2. Conditionals (Logic)

Syntax: `@?condition%true_text%false_text@`

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
`"txtHeader": "@?isAlert%WARNING%?isNorma%NORMAL%INFO@@"`
*(If isAlert=true -> WARNING, else if isNorma=true -> NORMAL, else INFO)*

---

## 3. Formatting Modifiers

### 3.1. Case Formatters
- `%toUpper`: CONVERTS TO UPPER CASE
- `%toLower`: converts to lower case
- `%toTitle`: Capitalizes Each Word

### 3.2. Numbers (printf)
Uses standard Go syntax:
- `%.1f`: 12.3
- `%d`: 123

### 3.3. Date and Time
- `%02.01.2006`: 02.05.2024
- `%15:04:05`: 14:30:05

---

## 4. Placeholder Reference (Go -> JSON)

Below are the primary keys called from the code and the variables available to them.

### 4.1. `msgStatus` and `msgAlertNotify`
These templates receive the full sensor state data.

**Data Variables:**
- `@date@`, `@time@`: Time object (requires format).
- `@deviceId@`, `@deviceName@`: Device ID and Name.
- `@aqiVal@`: Numeric AQI value.
- `@aqiLevel@`: Level (1-7). Used for icon building: `@icoLevel@aqiLevel@@`.
- `@aqiName@`: Localized level name.
- `@aqiStandardFlag@`: Reference to standard flag (`@flagUs@`/`@flagEu@`).
- `@val25@`, `@val10@`: Current PM values.
- `@diff25Percent@`, `@diff10Percent@`: Change in %.
- `@trend25Icon@`, `@trend10Icon@`: Trend icons (arrows).
- `@zone25Icon@`, `@zone10Icon@`: Current zone icons (squares).
- `@valT@`, `@valH@`, `@valP@`, `@valDp@`: Weather data.

**State Variables (Alerts only):**
- `@isAlert@`: `true` if air quality worsened.
- `@isNorma@`: `true` if air quality returned to normal.
- `@evt<EventName>@`: Specific event flags (e.g., `@evtVal10Yu@`, `@evtPm25Rise@`). Used in `txtAlertsList`.

### 4.2. `msgHelp`
- `@bot_version@`: Current bot version.

### 4.3. `msgAqiCycleMenu`
- `@vg1@`, `@vy1@`: Green/Yellow zone boundaries for PM2.5.
- `@vg2@`, `@vy2@`: Boundaries for PM10.

---

## 5. Development Rules

1. **No Logic in Go**: It is forbidden to build lists or choose icons in code. Use `evt*` flags and conditionals in JSON.
2. **Naming**: Always use `camelCase`.
3. **HTML**: Use native tags like `<b>`, `<i>`, `<code>`.
4. **Unicode**: Prohibit Unicode escaping (`\u003c`).
5. **Sorting**: JSON keys must be sorted topically:
   - Master templates (`msg*`)
   - Component texts (`txt*`)
   - Events (`evt*`)
   - Buttons (`btn*`)
   - Icons (`ico*`)
