# Dashboard Module

The `dashboard` module enables dynamic rendering and serialization of air quality metrics and historical charts onto high-contrast layout screens. It is specifically designed to feed e-Ink/EPD (Electrophoretic Display) hardware panels or display dynamic widgets as PNG/BMP images.

---

## Features

- **Format Serialization**: Supports logical bit-packing into binary `epd_raw` buffers (1-bit per pixel, MSB-first channels concatenated), standard `png`, and `bmp`.
- **Dynamic Hot Reload**: Dashboard layouts (`.yaml` configs) are re-parsed on every HTTP GET request, allowing instant visual feedback during development.
- **Dynamic Telemetry & History**: Renders instant sensor values, localized strings, calculated metrics (e.g., Dew Point, AQI indexes), and resampled historical charts.
- **Embedded Error Fallback**: Includes the high-contrast `Tamzen8x16r.ttf` font embedded inside the binary to render panic messages and connection issues on e-Ink screens.
- **Access Control List (ACL)**: Built-in CIDR subnet and single-IP validation directly in the layout file.

---

## Endpoint Specification

Dashboards are registered in the main configuration file under `dashboards` and served via HTTP GET.

**Query Parameters:**
- `format` (optional): `png`, `bmp`, `epd_raw`, or `epd_pr`. Overrides the default format specified in the dashboard YAML file.
- `mac` (optional): Used by `epd_pr` format to cache the previous frame for partial updates.
- `force_full` (optional): Used by `epd_pr` format to ignore the cache and force a full screen refresh.
- `device` / `device_id` (optional): The ID of the target sensor device. If omitted, falls back to the first device registered in the application.

**Example Request:**
```bash
curl -o screen.png "http://localhost:28288/dashboard/eink75?format=png&device=LivingRoom"
```

---

## Configuration Schema

Below is an annotated outline of a dashboard layout YAML file (e.g., `eink_800x480.yaml`):

```yaml
# Canvas Dimensions & Palette
screen:
  width: 800                # Width in pixels
  height: 480               # Height in pixels
  anti_aliasing: false      # Sharper lines on e-Ink if false
  palette:
    white: "#FFFFFF"
    black: "#000000"
    yellow: "#FFD500"
    red: "#FF0000"

lang: "en"                  # Translation dictionary (e.g. assets/en.json)

defaults:
  fit_text: true            # Shrink text if it exceeds container boundaries
  align:
    h: "center"             # Default horizontal align: left, center, right
    v: "center"             # Default vertical align: top, center, bottom

# Serialization Output Options
output:
  format: "epd_raw"         # Serialization type: epd_raw, png, bmp
  background: "white"       # Base canvas fill color
  mapping:                  # Logical EPD buffer mapping [Buffer1_Bit, Buffer2_Bit]
    black:  [0, 0]
    white:  [1, 0]
    yellow: [0, 1]
    red:    [1, 1]

# Access Control List
allowed:
  - "192.168.1.0/24"
  - "127.0.0.1"

# Fonts Definitions
fonts:
  aqi: { file: "fonts/PT-Root-UI_Bold.ttf", size: 100 }
  labels: { file: "fonts/PT-Root-UI_Medium.ttf", size: 20, hinting: "none" }

# Sequential Drawing Layers
layout:
  # Static/Templated Text Element
  - type: "text"
    position: { x: 20, y: 15 }
    size: { w: 180, h: 30 }
    font: { face: "labels", fg: "black" }
    template: "{date:02.01.2006} {time:15:04}" # Golang time layouts supported

  # Separator Line
  - type: "line"
    position: { x: 215, y: 20 }
    size: { w: 1, h: 440 }
    color: "black"

  # Sensor Value Card
  - type: "value"
    source: "pm25.value"
    format: "%.0f"
    position: { x: 20, y: 250 }
    size: { w: 180, h: 55 }
    value:
      font: { face: "values", fg: "black" }
      align: { v: "bottom" }
      thresholds:
        - { condition: "pm25.us.zone.index >= 3", fg: "white" }
    label:
      font: { face: "labels", fg: "black" }
      align: { v: "top" }
    unit:
      font: { face: "units", fg: "black" }
      align: { p: "right", s: 3, v: "1/2" }
    border:
      t: 2
      fg: "black"
      bg: "white"
      padding: 8
      affects: ["value", "unit"]
      thresholds:
        - { condition: "pm25.us.zone.index >= 3", bg: "red", fg: "red" }

  # Chart Element
  - type: "chart"
    position: { x: 260, y: 10 }
    size: { w: 520, h: 330 }
    source: "aqi.us.value"
    style: "bar"
    duration: "24h"
    points: 48
    axis:
      y:
        show: false
        min_step: 50
        labels: { show: true, count: 7, font: "axis_y" }
        notches: { show: true, fg: "black", w: 8, h: 2 }
      x:
        show: true
        labels: { show: true, count: 7, format: "15:04", font: "axis_x" }
        notches: { show: true, fg: "black", w: 2, h: 8 }
    threshold_lines:
      - { condition: "aqi.us.zone.index >= 3", color: "red", t: 2, style: "dashed.2.14" }
    thresholds:
      - { condition: "aqi.us.zone.index >= 3", bg: "red" }
```

---

## EPD Bit Packing Mechanics

When serialization format is set to `epd_raw`:
1. The canvas colors are matched to the closest palette color defined in `screen.palette` using RGB Euclidean distance.
2. The mapping rules define which bit (1 or 0) goes to which buffer. For example, a two-buffer mapping defines two output buffers:
   - `black: [0, 0]` writes `0` to Buffer 1, `0` to Buffer 2.
   - `white: [1, 0]` writes `1` to Buffer 1, `0` to Buffer 2.
3. Bits are packed MSB-first: 8 horizontal pixels are packed into a single byte.
4. The output HTTP payload is the concatenation of the buffers: `Buffer 1 bytes` followed immediately by `Buffer 2 bytes`.

### Partial Refresh (`epd_pr`)

The `epd_pr` format enables incremental updates to E-Ink screens. It generates the same bitplane data as `epd_raw` but limits the payload to the changed rectangular areas.

1. **In-Memory Caching:** The server caches the last rendered `image.Image` state for each device based on its `mac` address query parameter.
2. **Difference Bounding Boxes:** When requested, the server compares the cached state with the newly rendered state using an 8x8 pixel grid. It finds all changed pixels and creates the minimal number of connected bounding boxes.
3. **8x8 Grid Benefits:** Bounding boxes are guaranteed to have coordinates and widths that are multiples of 8. This aligns the start and end of every tile row perfectly to byte boundaries, avoiding complex bit shifts during bitplane encoding and client-side extraction.
4. **Binary Protocol (Little-Endian):**
   - The payload starts with a 2-byte header `N` (number of tiles).
   - Followed by `N` tile headers, each containing 4 unsigned 16-bit integers: `[X] [Y] [Width] [Height]`.
   - The header is immediately followed by the packed bitplane data for the tile.
5. **Full Refresh Fallback:** If the `mac` is not in cache, or `force_full=true` is provided, the server outputs `N=1` and a single tile covering the entire screen `X=0, Y=0, W=Width, H=Height`.

---

## High Contrast Error Rendering

If the server encounters a layout syntax error, database connection failure, or request parsing panic:
- An image is generated with the layout's dimensions.
- The error description is drawn in a high-contrast format.
- The font used is the embedded `Tamzen8x16r.ttf` monospace font, ensuring readability even on hardware screens.
