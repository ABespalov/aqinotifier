# Localization System Documentation (v2.1)

The system uses a **recursive block-based template engine** with logic support within templates.

---

## 1. Core Principle: Logic in Templates
UI construction (icons, titles, lists) has been moved from Go code to JSON files.

### 1.1. Nested Conditionals (Switch)
If the third part of a condition starts with `?`, it's treated as a nested conditional, enabling `switch` behavior.

**Example:**
```json
"txtAlertHeader": "@?isAlert%@icoAlert@ WARNING%?isNorma%@icoSuccess@ NORMAL%@icoInfo@ INFO@@"
```

---

## 2. Atomic Data (from Go)
The bot provides only raw data and state flags.

| Placeholder | Description |
| :--- | :--- |
| @aqiLevel@ | AQI Level (1-7). Used for recursive key building: `@icoLevel@aqiLevel@@`. |
| @isAlert@ | Critical alert flag (bool). |
| @isNorma@ | Normalization flag (bool). |
| @evt_name_id@ | Active event flags (e.g., @evt_val10_yu@). |
| @aqiVal@, @val25@... | Numeric indicators. |

---

## 3. No Virtual Placeholders
Placeholders like `@icon@`, `@title@`, and `@alerts@` are prohibited as they imply code-side formatting.
Use JSON logic instead:
- Icons: `@icoLevel@aqiLevel@@`.
- Headers: conditionals based on `isAlert` / `isNorma`.
- Lists: chains of conditionals based on `evt_...` flags.

---

## 4. Formatting Rules
1. **No Unicode Escaping**: Use native HTML and plain text only.
2. **Topical Sorting**: Keys are grouped by context (Master templates -> Components -> Buttons -> System).
