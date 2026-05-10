# Localization System Documentation (v2.2)

The localization system is based on a powerful **recursive block template engine**, allowing all display logic (icons, conditions, inflections) to be moved from Go code to JSON files.

---

## 1. Template Syntax

Basic placeholder format: `@key%format@`

### 1.1. Case Formatters
- `%toUpper`: CONVERTS TO UPPER CASE
- `%toLower`: converts to lower case
- `%toTitle`: Capitalizes The First Letter

### 1.2. Sub-templates with Arguments (Components)
You can call one key inside another and pass parameters to it. This allows for message "constructors" and avoids duplication.

**Syntax:** `@SubTemplate%arg1=value1%arg2=value2@`

**Example:**
- `txtEvtRiseTo`: `@actionRise@ @pm@ to @zone@ zone`
- `evtVal10Yu`: `@txtEvtRiseTo%pm=@labelPm10@%zone=Yellow@@`
*Here `txtEvtRiseTo` is called with injected values for `pm` and `zone`.*

---

## 2. Conditional Logic (Logic)
... (standard logic docs)
