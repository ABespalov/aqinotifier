package tgbot

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/go-analyze/charts"
)

var (
	colorGreenZone  = charts.Color{R: 0, G: 255, B: 0, A: 85}   // 33% opacity
	colorYellowZone = charts.Color{R: 255, G: 255, B: 0, A: 85} // 33% opacity
	colorRedZone    = charts.Color{R: 255, G: 0, B: 0, A: 85}   // 33% opacity

	colorSeriesRed    = charts.ParseColor("#80090799") // Red
	colorSeriesBlue   = charts.ParseColor("#0c4c8084") // Blue
	colorSeriesPurple = charts.ParseColor("#6d197c8c") // Purple
	colorSeriesGrey   = charts.ParseColor("#505050")   // Wet asphalt

	// AQI Colors
	colorAQIGood      = charts.ParseColor("#00E400")
	colorAQILightBlue = charts.ParseColor("#52B6E6")
	colorAQIModerate  = charts.ParseColor("#FFFF00")
	colorAQISlightly  = charts.ParseColor("#FF7E00")
	colorAQIUnhealthy = charts.ParseColor("#FF0000")
	colorAQIVery      = charts.ParseColor("#8F3F97")
	colorAQIHazardous = charts.ParseColor("#7E0023")
	colorAQIExtreme   = charts.ParseColor("#505050")
)

// generateCharts produces a slice of PNG buffers containing PM, Temperature,
// Humidity, and Pressure charts based on the provided measurement history.
const (
	chartStrokeWidth = 3.0
	chartSmoothing   = 0.4

	// Padding coefficients (relative to fontSize)
	chartPadLeft   = 3.5
	chartPadRight  = 6.0
	chartPadTop    = 4.0
	chartPadBottom = 5.5

	// Axis width coefficients (additive model)
	chartAxisDigitWeight = 0.9
	chartAxisSepWeight   = 0.3
	chartAxisBase        = 0.85

	// Geometric coefficients (relative to fontSize or chartWidth)
	chartXAxisHeightCoef = 2.0
	chartTitleHeightCoef = 3.0
	chartLabelFontCoef   = 0.8
	chartBarWidthCoef    = 0.01 // relative to chartWidth
	chartTextHalfLenCoef = 5.0  // for centering vertical labels
	chartLabelXOffsetL   = 0.6
	chartLabelXOffsetR   = 0.4

	// Scaling and margin coefficients
	chartHeadroomCoef    = 0.1 // 10% margin
	chartTitleFontCoef   = 1.2
	chartDefaultMin      = 0.0
	chartDefaultMax      = 10.0
	chartLabelColorAlpha = 255

	// Drawing coefficients
	chartThresholdPaddingCoef = 3.0
	chartDotLargeCoef         = 2.4
	chartDotSmallCoef         = 0.8
	chartDashWidthCoef        = 0.35

	// Data and aggregation logic
	chartAggregationWindow = 15 * time.Minute
	chartTimeLabelStep     = 3 // Hours between labels on X axis

	// Conversion constants
	celsiusToFahrenheitSlope  = 1.8
	celsiusToFahrenheitOffset = 32.0

	// Magnus formula constants (for dew point calculation)
	magnusB = 17.27
	magnusC = 237.7
)

var (
	colorAxisLabel   = charts.Color{R: 31, G: 31, B: 31, A: chartLabelColorAlpha}
	chartDashPattern = []float64{4, 4}
)

// chartFormatter standardizes value labels across all charts.
func chartFormatter(f float64, isAQI bool) string {
	if isAQI {
		return fmt.Sprintf("%d", int(math.Round(f)))
	}
	return fmt.Sprintf("%.1f", f)
}

// calcYAxisWidth returns the pixel width needed for the Y-axis labels.
func calcYAxisWidth(fs float64, yMin, yMax float64, isAQI bool) int {
	s1 := chartFormatter(yMin, isAQI)
	s2 := chartFormatter(yMax, isAQI)
	s := s1
	if len(s2) > len(s1) {
		s = s2
	}
	var w float64
	for _, r := range s {
		if r == '.' || r == ',' || r == ':' {
			w += chartAxisSepWeight
		} else {
			w += chartAxisDigitWeight
		}
	}
	return int(fs * (w + chartAxisBase))
}

func generateCharts(b *Bot, chatID int64, hist []monitor.Measurement, chartWidth, chartHeight int, chartFontSize float64) ([][]byte, error) {
	if len(hist) == 0 {
		return nil, nil
	}

	var labels []string
	var pm10Values, pm25Values []float64
	var aqiValues []float64
	var tempValues, humValues, pressValues, dewPointValues []float64

	// Resampling removed to show all history points as is.

	isF := b.store.GetUnitTemp(chatID) == "f"
	mcfg := b.GetUserSettings(chatID)
	for i, m := range hist {
		local := m.Timestamp.Local()
		label := ""
		// Show label every 3 points for short history to ensure the X-axis isn't empty
		if i%3 == 0 {
			label = local.Format("15:04:05") // Added seconds as points might be close
		}
		labels = append(labels, label)
		pm10Values = append(pm10Values, m.PM10)
		pm25Values = append(pm25Values, m.PM25)

		var aqi float64
		if mcfg.AQIStandard == "US" {
			aqi, _ = sensor.CalculateUS_AQI(m.PM25, m.PM10)
		} else {
			aqi, _ = sensor.CalculateEU_AQI(m.PM25, m.PM10)
		}
		aqiValues = append(aqiValues, aqi)

		if m.Temperature != 0 {
			t := b.convertTemp(m.Temperature, chatID)
			tempValues = append(tempValues, t)
			if m.Humidity != 0 {
				dp := CalcDewPoint(m.Temperature, m.Humidity)
				if isF {
					dp = dp*celsiusToFahrenheitSlope + celsiusToFahrenheitOffset
				}
				dewPointValues = append(dewPointValues, dp)
			} else {
				dewPointValues = append(dewPointValues, charts.GetNullValue())
			}
		} else {
			tempValues = append(tempValues, charts.GetNullValue())
			dewPointValues = append(dewPointValues, charts.GetNullValue())
		}

		if m.Humidity != 0 {
			humValues = append(humValues, m.Humidity)
		} else {
			humValues = append(humValues, charts.GetNullValue())
		}

		if m.Pressure != 0 {
			pressValues = append(pressValues, b.convertPress(m.Pressure, chatID))
		} else {
			pressValues = append(pressValues, charts.GetNullValue())
		}
	}

	pmTitle := b.T(chatID, "chart_pm_title")

	buildChart := func(title, yAxisName string, seriesNames []string, data [][]float64, forceZero bool) ([]byte, error) {
		isPM := strings.Contains(strings.ToLower(title), "pm")
		isAQI := strings.Contains(strings.ToLower(title), "aqi")
		theme := charts.GetDefaultTheme()

		// Calculate Y axis range strictly from data curves
		var yMin, yMax float64 = math.MaxFloat64, -math.MaxFloat64
		hasData := false
		for _, series := range data {
			for _, v := range series {
				if v != charts.GetNullValue() {
					if v > yMax {
						yMax = v
					}
					if v < yMin {
						yMin = v
					}
					hasData = true
				}
			}
		}

		if !hasData {
			yMin, yMax = chartDefaultMin, chartDefaultMax
		} else if forceZero {
			yMin = 0
			yMax *= (1.0 + chartHeadroomCoef)
		} else {
			// Add headroom margin for other charts
			span := yMax - yMin
			if span == 0 {
				yMin -= 1
				yMax += 1
			} else {
				yMin -= span * chartHeadroomCoef
				yMax += span * chartHeadroomCoef
			}
		}

		// Fixed margin from the image edge to the start of labels.
		pLeft := chartFontSize * chartPadLeft
		pRight := chartFontSize * chartPadRight
		pTop := chartFontSize * chartPadTop
		pBottom := chartFontSize * chartPadBottom

		opt := charts.NewLineChartOptionWithData(data)
		opt.Padding = charts.Box{
			Left:   int(pLeft),
			Right:  int(pRight),
			Top:    int(pTop),
			Bottom: int(pBottom),
		}
		opt.Title = charts.TitleOption{
			Text:      fmt.Sprintf("%s (%s)", title, yAxisName),
			FontStyle: charts.FontStyle{FontSize: chartFontSize * chartTitleFontCoef},
		}
		opt.XAxis = charts.XAxisOption{
			Show:        charts.Ptr(true),
			BoundaryGap: charts.Ptr(false),
			Labels:      labels,
			LabelFontStyle: charts.FontStyle{
				FontSize:  chartFontSize,
				FontColor: colorAxisLabel,
			},
		}
		opt.YAxis = []charts.YAxisOption{
			{
				Min:            charts.Ptr(yMin),
				Max:            charts.Ptr(yMax),
				Show:           charts.Ptr(true),
				ValueFormatter: func(f float64) string { return chartFormatter(f, isAQI) },
				LabelFontStyle: charts.FontStyle{FontSize: chartFontSize},
			},
		}
		opt.Legend = charts.LegendOption{
			FontStyle: charts.FontStyle{FontSize: chartFontSize},
		}
		opt.LineStrokeWidth = chartStrokeWidth
		opt.StrokeSmoothingTension = chartSmoothing

		p := charts.NewPainter(charts.PainterOptions{
			Width:  chartWidth,
			Height: chartHeight,
		})
		p.FillArea([]charts.Point{
			{X: 0, Y: 0},
			{X: chartWidth, Y: 0},
			{X: chartWidth, Y: chartHeight},
			{X: 0, Y: chartHeight},
			{X: 0, Y: 0},
		}, charts.ColorWhite)

		if isPM {
			// Main chart styling
			opt.Theme = charts.GetDefaultTheme().WithSeriesColors([]charts.Color{colorSeriesRed, colorSeriesBlue})
			opt.Theme = opt.Theme.WithBackgroundColor(charts.ColorTransparent)
			for i := 0; i < len(opt.SeriesList); i++ {
				opt.SeriesList[i].Name = seriesNames[i]
			}
		} else if isAQI {
			opt.Theme = theme.WithSeriesColors([]charts.Color{colorSeriesGrey})
			for i, name := range seriesNames {
				opt.SeriesList[i].Name = name
			}
		} else {
			colors := []charts.Color{colorSeriesBlue}
			if title == b.T(chatID, "msg_temp") {
				colors = []charts.Color{colorSeriesRed, colorSeriesBlue}
			} else if title == b.T(chatID, "msg_press") {
				colors = []charts.Color{colorSeriesPurple}
			}
			opt.Theme = theme.WithSeriesColors(colors)
			for i, name := range seriesNames {
				opt.SeriesList[i].Name = name
			}
		}

		if err := p.LineChart(opt); err != nil {
			return nil, err
		}

		if isPM {
			// 1. Get the content painter (excludes outer Padding)
			cp := p.Child(charts.PainterPaddingOption(opt.Padding))

			yAxisWidth := calcYAxisWidth(chartFontSize, yMin, yMax, isAQI)
			xAxisHeight := int(chartFontSize * chartXAxisHeightCoef)
			titleHeight := int(chartFontSize * chartTitleHeightCoef)

			// 3. Create a child painter for the ACTUAL grid area (the "pure" area)
			pureCp := cp.Child(charts.PainterPaddingOption(charts.Box{
				Left:   yAxisWidth,
				Bottom: xAxisHeight,
				Top:    titleHeight,
			}))

			gridW := float64(pureCp.Width())
			gridH := float64(pureCp.Height())

			// 4. Vertical axis labels (rotated and centered)
			styleLeft := charts.FontStyle{
				FontSize:  chartFontSize * chartLabelFontCoef,
				FontColor: colorSeriesRed,
			}
			styleRight := charts.FontStyle{
				FontSize:  chartFontSize * chartLabelFontCoef,
				FontColor: colorSeriesBlue,
			}

			barWidth := float64(chartWidth) * chartBarWidthCoef
			// Center vertically: subtract approx half-length of vertical labels
			textHalfLen := int(chartFontSize * chartLabelFontCoef * chartTextHalfLenCoef)

			// PM2.5 label: inside the grid, just to the right of the bar (Red, Left)
			pureCp.Text(b.T(chatID, "chart_scale_pm25"), int(barWidth+chartFontSize*chartLabelXOffsetL), int(gridH/2)-textHalfLen, math.Pi/2, styleLeft)

			// PM10 label: inside the grid, near the right bar (Blue, Right)
			pureCp.Text(b.T(chatID, "chart_scale_pm10"), int(gridW-barWidth-chartFontSize*chartLabelXOffsetR), int(gridH/2)+textHalfLen, -math.Pi/2, styleRight)

			// Vertical range: exactly from 0 to gridH in pure coordinates
			yAxisBottom := gridH
			yAxisTop := 0.0
			plotHeight := yAxisBottom - yAxisTop

			yFunc := func(val float64) float64 {
				y := yAxisBottom - (val-yMin)/(yMax-yMin)*plotHeight
				if y < yAxisTop {
					y = yAxisTop
				}
				if y > yAxisBottom {
					y = yAxisBottom
				}
				return y
			}

			drawBar := func(x1, x2 float64, low, high float64, color charts.Color) {
				start := math.Max(low, yMin)
				end := math.Min(high, yMax)
				if start < end {
					pyBottom := yFunc(start)
					pyTop := yFunc(end)
					pureCp.FillArea([]charts.Point{
						{X: int(x1), Y: int(pyBottom)},
						{X: int(x2), Y: int(pyBottom)},
						{X: int(x2), Y: int(pyTop)},
						{X: int(x1), Y: int(pyTop)},
						{X: int(x1), Y: int(pyBottom)},
					}, color)
				}
			}

			// PM10 bar on the left: exactly at the start of the grid (0 in pure coords)
			drawBar(0, barWidth, 0, mcfg.PM25Green, colorGreenZone)
			drawBar(0, barWidth, mcfg.PM25Green, mcfg.PM25Yellow, colorYellowZone)
			drawBar(0, barWidth, mcfg.PM25Yellow, math.MaxFloat64, colorRedZone)

			// PM2.5 bar on the right: exactly at the end of the grid (gridW in pure coords)
			drawBar(gridW-barWidth, gridW, 0, mcfg.PM10Green, colorGreenZone)
			drawBar(gridW-barWidth, gridW, mcfg.PM10Green, mcfg.PM10Yellow, colorYellowZone)
			drawBar(gridW-barWidth, gridW, mcfg.PM10Yellow, math.MaxFloat64, colorRedZone)

			dashWidth := chartStrokeWidth * chartDashWidthCoef
			dashPattern := chartDashPattern

			drawThreshold := func(val float64, color charts.Color, isLeft bool) {
				if val < yMin || val > yMax {
					return
				}
				y := yFunc(val)
				var tx1, tx2 float64
				if isLeft {
					tx1 = 0.0
					tx2 = gridW - barWidth*chartThresholdPaddingCoef
				} else {
					tx1 = barWidth * chartThresholdPaddingCoef
					tx2 = gridW
				}
				pureCp.DashedLineStroke([]charts.Point{{X: int(tx1), Y: int(y)}, {X: int(tx2), Y: int(y)}}, color, dashWidth, dashPattern)
			}

			// 5. Draw intersection dots where data crosses thresholds
			drawDots := func(seriesData []float64, threshold float64, color charts.Color) {
				if len(seriesData) < 2 {
					return
				}
				yT := yFunc(threshold)
				for i := 0; i < len(seriesData)-1; i++ {
					v1 := seriesData[i]
					v2 := seriesData[i+1]
					// Check crossing
					if (v1 <= threshold && v2 >= threshold) || (v1 >= threshold && v2 <= threshold) {
						if v1 == v2 {
							continue
						}
						t := (threshold - v1) / (v2 - v1)
						x1 := float64(i) / float64(len(seriesData)-1) * gridW
						x2 := float64(i+1) / float64(len(seriesData)-1) * gridW
						x := x1 + t*(x2-x1)
						// Draw inner white dot inside a larger colored dot
						pureCp.Circle(chartStrokeWidth*chartDotLargeCoef, int(x), int(yT), color, color, 0)
						if !isAQI {
							pureCp.Circle(chartStrokeWidth*chartDotSmallCoef, int(x), int(yT), charts.ColorWhite, charts.ColorWhite, 0)
						}
					}
				}
			}

			// Draw PM10 thresholds and dots
			drawThreshold(mcfg.PM25Green, colorSeriesRed, true)
			drawDots(data[0], mcfg.PM25Green, colorSeriesRed)
			drawThreshold(mcfg.PM25Yellow, colorSeriesRed, true)
			drawDots(data[0], mcfg.PM25Yellow, colorSeriesRed)

			// Draw PM10 thresholds and dots (with small offset if identical to PM10)
			g10 := mcfg.PM10Green
			if g10 == mcfg.PM25Green {
				g10 -= (yMax - yMin) / plotHeight
			}
			drawThreshold(g10, colorSeriesBlue, false)
			drawDots(data[1], g10, colorSeriesBlue)

			y10 := mcfg.PM10Yellow
			if y10 == mcfg.PM25Yellow {
				y10 -= (yMax - yMin) / plotHeight
			}
			drawThreshold(y10, colorSeriesBlue, false)
			drawDots(data[1], y10, colorSeriesBlue)
		}

		if isAQI {
			cp := p.Child(charts.PainterPaddingOption(opt.Padding))
			yAxisWidth := calcYAxisWidth(chartFontSize, yMin, yMax, isAQI)
			xAxisHeight := int(chartFontSize * chartXAxisHeightCoef)
			titleHeight := int(chartFontSize * chartTitleHeightCoef)

			pureCp := cp.Child(charts.PainterPaddingOption(charts.Box{
				Left:   yAxisWidth,
				Bottom: xAxisHeight,
				Top:    titleHeight,
			}))

			gridW := float64(pureCp.Width())
			gridH := float64(pureCp.Height())
			yAxisBottom := gridH
			yAxisTop := 0.0
			plotHeight := yAxisBottom - yAxisTop

			yFunc := func(val float64) float64 {
				y := yAxisBottom - (val-yMin)/(yMax-yMin)*plotHeight
				if y < yAxisTop {
					y = yAxisTop
				}
				if y > yAxisBottom {
					y = yAxisBottom
				}
				return y
			}

			mcfg = b.GetUserSettings(chatID)
			breakpoints := sensor.IndexPointsEU
			if mcfg.AQIStandard == "US" {
				breakpoints = sensor.IndexPointsUS
			}

			colors := []charts.Color{
				colorAQIGood, colorAQIModerate, colorAQISlightly,
				colorAQIUnhealthy, colorAQIVery, colorAQIHazardous, colorAQIExtreme,
			}
			if mcfg.AQIStandard == "EU" {
				colors = []charts.Color{
					colorAQILightBlue, colorAQIGood, colorAQIModerate,
					colorAQISlightly, colorAQIUnhealthy, colorAQIHazardous,
				}
			}

			// Draw background zones
			for i := 0; i < len(breakpoints)-1; i++ {
				low := breakpoints[i]
				high := breakpoints[i+1]
				if i == len(breakpoints)-2 && mcfg.AQIStandard == "US" {
					high = sensor.IndexPointsUS[len(sensor.IndexPointsUS)-1]
				}

				c := colors[i]
				c.A = 51 // ~20% opacity (255 * 0.2)

				pyBottom := yFunc(math.Max(low, yMin))
				pyTop := yFunc(math.Min(high, yMax))

				if pyBottom > pyTop {
					pureCp.FillArea([]charts.Point{
						{X: 0, Y: int(pyBottom)},
						{X: int(gridW), Y: int(pyBottom)},
						{X: int(gridW), Y: int(pyTop)},
						{X: 0, Y: int(pyTop)},
						{X: 0, Y: int(pyBottom)},
					}, c)
				}
			}

			// Draw dashed lines
			// 5. Draw intersection dots where data crosses thresholds
			drawDots := func(seriesData []float64, threshold float64, color charts.Color) {
				if len(seriesData) < 2 {
					return
				}
				yT := yFunc(threshold)
				for i := 0; i < len(seriesData)-1; i++ {
					v1 := seriesData[i]
					v2 := seriesData[i+1]
					if (v1 <= threshold && v2 >= threshold) || (v1 >= threshold && v2 <= threshold) {
						if v1 == v2 {
							continue
						}
						t := (threshold - v1) / (v2 - v1)
						x1 := float64(i) / float64(len(seriesData)-1) * gridW
						x2 := float64(i+1) / float64(len(seriesData)-1) * gridW
						x := x1 + t*(x2-x1)
						pureCp.Circle(chartStrokeWidth*2.4, int(x), int(yT), color, color, 0)

					}
				}
			}

			dashWidth := chartStrokeWidth * chartDashWidthCoef
			dashPattern := chartDashPattern
			for i := 1; i < len(breakpoints); i++ {
				val := breakpoints[i]
				if val < yMin || val > yMax {
					continue
				}
				y := yFunc(val)
				pureCp.DashedLineStroke([]charts.Point{{X: 0, Y: int(y)}, {X: int(gridW), Y: int(y)}}, colors[i-1], dashWidth, dashPattern)
				drawDots(data[0], val, colorSeriesGrey)
			}
		}

		return p.Bytes()
	}

	var buffers [][]byte

	aqiTitle := b.T(chatID, "btn_chart_aqi", "")
	aqiBuf, err := buildChart(aqiTitle, mcfg.AQIStandard, []string{"AQI"}, [][]float64{aqiValues}, true)
	if err == nil {
		buffers = append(buffers, aqiBuf)
	}

	pmBuf, err := buildChart(pmTitle, b.T(chatID, "chart_unit_pm"),
		[]string{"PM2.5", "PM10"},
		[][]float64{pm25Values, pm10Values}, true)
	if err != nil {
		return nil, err
	}
	buffers = append(buffers, pmBuf)

	if len(tempValues) > 0 {
		buf, err := buildChart(b.T(chatID, "msg_temp"), b.unitTempLabel(chatID),
			[]string{b.T(chatID, "msg_temp"), b.T(chatID, "msg_dew_point")},
			[][]float64{tempValues, dewPointValues}, false)
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	if len(humValues) > 0 {
		buf, err := buildChart(b.T(chatID, "msg_hum"), "%", []string{b.T(chatID, "msg_hum")}, [][]float64{humValues}, false)
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	if len(pressValues) > 0 {
		buf, err := buildChart(b.T(chatID, "msg_press"), b.unitPressLabel(chatID), []string{b.T(chatID, "msg_press")}, [][]float64{pressValues}, false)
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	return buffers, nil
}

// generateSingleChart produces a single PNG buffer containing the requested chart type
// for the last 24 hours.
func generateSingleChart(b *Bot, chatID int64, hist []monitor.Measurement, chartType string, chartWidth, chartHeight int, chartFontSize float64) ([]byte, error) {
	if len(hist) == 0 {
		return nil, nil
	}

	filteredHist := hist

	// Resample to 15-minute intervals for better chart readability and stable labels
	window := chartAggregationWindow
	type bucket struct {
		pm10, pm25, temp, hum, press float64
		count                        int
	}
	buckets := make(map[int64]*bucket)
	var keys []int64
	for _, m := range filteredHist {
		k := m.Timestamp.Truncate(window).Unix()
		if _, ok := buckets[k]; !ok {
			keys = append(keys, k)
			buckets[k] = &bucket{}
		}
		b := buckets[k]
		b.pm10 += m.PM10
		b.pm25 += m.PM25
		b.temp += m.Temperature
		b.hum += m.Humidity
		b.press += m.Pressure
		b.count++
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	var resampled []monitor.Measurement
	for _, k := range keys {
		b := buckets[k]
		resampled = append(resampled, monitor.Measurement{
			Timestamp:   time.Unix(k, 0),
			PM10:        b.pm10 / float64(b.count),
			PM25:        b.pm25 / float64(b.count),
			Temperature: b.temp / float64(b.count),
			Humidity:    b.hum / float64(b.count),
			Pressure:    b.press / float64(b.count),
		})
	}
	filteredHist = resampled

	var labels []string
	var pm10Values, pm25Values []float64
	var aqiValues []float64
	var tempValues, humValues, pressValues, dewPointValues []float64

	lastHour := -1
	isF := b.store.GetUnitTemp(chatID) == "f"
	mcfg := b.GetUserSettings(chatID)

	for _, m := range filteredHist {
		local := m.Timestamp.Local()
		label := ""
		// Add label every 3 hours (0, 3, 6, 9, 12, 15, 18, 21)
		if local.Hour()%chartTimeLabelStep == 0 && local.Hour() != lastHour {
			label = local.Format("15:04")
			lastHour = local.Hour()
		}
		labels = append(labels, label)

		switch chartType {
		case "pm":
			pm10Values = append(pm10Values, m.PM10)
			pm25Values = append(pm25Values, m.PM25)
		case "aqi":
			var aqi float64
			if mcfg.AQIStandard == "US" {
				aqi, _ = sensor.CalculateUS_AQI(m.PM25, m.PM10)
			} else {
				aqi, _ = sensor.CalculateEU_AQI(m.PM25, m.PM10)
			}
			aqiValues = append(aqiValues, aqi)
		case "temp":
			if m.Temperature != 0 {
				tempValues = append(tempValues, b.convertTemp(m.Temperature, chatID))
				if m.Humidity != 0 {
					dp := CalcDewPoint(m.Temperature, m.Humidity)
					if isF {
						dp = dp*1.8 + 32
					}
					dewPointValues = append(dewPointValues, dp)
				} else {
					dewPointValues = append(dewPointValues, charts.GetNullValue())
				}
			} else {
				tempValues = append(tempValues, charts.GetNullValue())
				dewPointValues = append(dewPointValues, charts.GetNullValue())
			}
		case "hum":
			if m.Humidity != 0 {
				humValues = append(humValues, m.Humidity)
			} else {
				humValues = append(humValues, charts.GetNullValue())
			}
		case "press":
			if m.Pressure != 0 {
				pressValues = append(pressValues, b.convertPress(m.Pressure, chatID))
			} else {
				pressValues = append(pressValues, charts.GetNullValue())
			}
		}
	}

	pmTitle := b.T(chatID, "chart_pm_title")

	buildChart := func(title, yAxisName string, seriesNames []string, data [][]float64, forceZero bool) ([]byte, error) {
		isPM := strings.Contains(strings.ToLower(title), "pm")
		isAQI := strings.Contains(strings.ToLower(title), "aqi")
		theme := charts.GetDefaultTheme()
		// Calculate Y axis range strictly from data curves
		var yMin, yMax float64 = math.MaxFloat64, -math.MaxFloat64
		hasData := false
		for _, series := range data {
			for _, v := range series {
				if v != charts.GetNullValue() {
					if v > yMax {
						yMax = v
					}
					if v < yMin {
						yMin = v
					}
					hasData = true
				}
			}
		}

		if !hasData {
			yMin, yMax = chartDefaultMin, chartDefaultMax
		} else if forceZero {
			yMin = 0
			yMax *= (1.0 + chartHeadroomCoef)
		} else {
			// Add headroom margin for other charts
			span := yMax - yMin
			if span == 0 {
				yMin -= 1
				yMax += 1
			} else {
				yMin -= span * chartHeadroomCoef
				yMax += span * chartHeadroomCoef
			}
		}

		// Fixed margin from the image edge to the start of labels.
		pLeft := chartFontSize * chartPadLeft
		pRight := chartFontSize * chartPadRight
		pTop := chartFontSize * chartPadTop
		pBottom := chartFontSize * chartPadBottom

		opt := charts.NewLineChartOptionWithData(data)
		opt.Padding = charts.Box{
			Left:   int(pLeft),
			Right:  int(pRight),
			Top:    int(pTop),
			Bottom: int(pBottom),
		}
		opt.Title = charts.TitleOption{
			Text:      fmt.Sprintf("%s (%s)", title, yAxisName),
			FontStyle: charts.FontStyle{FontSize: chartFontSize * chartTitleFontCoef},
		}
		opt.XAxis = charts.XAxisOption{
			Show:        charts.Ptr(true),
			BoundaryGap: charts.Ptr(false),
			Labels:      labels,
			LabelFontStyle: charts.FontStyle{
				FontSize:  chartFontSize,
				FontColor: colorAxisLabel,
			},
		}
		opt.YAxis = []charts.YAxisOption{
			{
				Min:            charts.Ptr(yMin),
				Max:            charts.Ptr(yMax),
				Show:           charts.Ptr(true),
				ValueFormatter: func(f float64) string { return chartFormatter(f, isAQI) },
				LabelFontStyle: charts.FontStyle{FontSize: chartFontSize},
			},
		}
		opt.Legend = charts.LegendOption{
			FontStyle: charts.FontStyle{FontSize: chartFontSize},
		}
		opt.LineStrokeWidth = chartStrokeWidth
		opt.StrokeSmoothingTension = chartSmoothing

		p := charts.NewPainter(charts.PainterOptions{
			Width:  chartWidth,
			Height: chartHeight,
		})
		p.FillArea([]charts.Point{
			{X: 0, Y: 0},
			{X: chartWidth, Y: 0},
			{X: chartWidth, Y: chartHeight},
			{X: 0, Y: chartHeight},
			{X: 0, Y: 0},
		}, charts.ColorWhite)

		if isPM {
			// Main chart styling
			opt.Theme = charts.GetDefaultTheme().WithSeriesColors([]charts.Color{colorSeriesRed, colorSeriesBlue})
			opt.Theme = opt.Theme.WithBackgroundColor(charts.ColorTransparent)
			for i := 0; i < len(opt.SeriesList); i++ {
				opt.SeriesList[i].Name = seriesNames[i]
			}
		} else if isAQI {
			opt.Theme = theme.WithSeriesColors([]charts.Color{colorSeriesGrey})
			for i := 0; i < len(opt.SeriesList); i++ {
				opt.SeriesList[i].Name = seriesNames[i]
			}
		} else {
			colors := []charts.Color{colorSeriesBlue}
			if title == b.T(chatID, "msg_temp") {
				colors = []charts.Color{colorSeriesRed, colorSeriesBlue}
			} else if title == b.T(chatID, "msg_press") {
				colors = []charts.Color{colorSeriesPurple}
			}
			opt.Theme = theme.WithSeriesColors(colors)
			for i, name := range seriesNames {
				opt.SeriesList[i].Name = name
			}
		}

		if err := p.LineChart(opt); err != nil {
			return nil, err
		}

		if isPM {
			// 1. Get the content painter (excludes outer Padding)
			cp := p.Child(charts.PainterPaddingOption(opt.Padding))

			yAxisWidth := calcYAxisWidth(chartFontSize, yMin, yMax, isAQI)
			xAxisHeight := int(chartFontSize * chartXAxisHeightCoef)
			titleHeight := int(chartFontSize * chartTitleHeightCoef)

			// 3. Create a child painter for the ACTUAL grid area (the "pure" area)
			pureCp := cp.Child(charts.PainterPaddingOption(charts.Box{
				Left:   yAxisWidth,
				Bottom: xAxisHeight,
				Top:    titleHeight,
			}))

			gridW := float64(pureCp.Width())
			gridH := float64(pureCp.Height())

			// 4. Vertical axis labels (rotated and centered)
			styleLeft := charts.FontStyle{
				FontSize:  chartFontSize * chartLabelFontCoef,
				FontColor: colorSeriesRed,
			}
			styleRight := charts.FontStyle{
				FontSize:  chartFontSize * chartLabelFontCoef,
				FontColor: colorSeriesBlue,
			}

			barWidth := float64(chartWidth) * chartBarWidthCoef
			// Center vertically: subtract approx half-length of vertical labels
			textHalfLen := int(chartFontSize * chartLabelFontCoef * chartTextHalfLenCoef)

			// PM2.5 label: inside the grid, just to the right of the bar (Red, Left)
			pureCp.Text(b.T(chatID, "chart_scale_pm25"), int(barWidth+chartFontSize*chartLabelXOffsetL), int(gridH/2)-textHalfLen, math.Pi/2, styleLeft)

			// PM10 label: inside the grid, near the right bar (Blue, Right)
			pureCp.Text(b.T(chatID, "chart_scale_pm10"), int(gridW-barWidth-chartFontSize*chartLabelXOffsetR), int(gridH/2)+textHalfLen, -math.Pi/2, styleRight)

			// Vertical range: exactly from 0 to gridH in pure coordinates
			yAxisBottom := gridH
			yAxisTop := 0.0
			plotHeight := yAxisBottom - yAxisTop

			yFunc := func(val float64) float64 {
				y := yAxisBottom - (val-yMin)/(yMax-yMin)*plotHeight
				if y < yAxisTop {
					y = yAxisTop
				}
				if y > yAxisBottom {
					y = yAxisBottom
				}
				return y
			}

			drawBar := func(x1, x2 float64, low, high float64, color charts.Color) {
				start := math.Max(low, yMin)
				end := math.Min(high, yMax)
				if start < end {
					pyBottom := yFunc(start)
					pyTop := yFunc(end)
					pureCp.FillArea([]charts.Point{
						{X: int(x1), Y: int(pyBottom)},
						{X: int(x2), Y: int(pyBottom)},
						{X: int(x2), Y: int(pyTop)},
						{X: int(x1), Y: int(pyTop)},
						{X: int(x1), Y: int(pyBottom)},
					}, color)
				}
			}

			// PM10 bar on the left: exactly at the start of the grid (0 in pure coords)
			drawBar(0, barWidth, 0, mcfg.PM25Green, colorGreenZone)
			drawBar(0, barWidth, mcfg.PM25Green, mcfg.PM25Yellow, colorYellowZone)
			drawBar(0, barWidth, mcfg.PM25Yellow, math.MaxFloat64, colorRedZone)

			// PM2.5 bar on the right: exactly at the end of the grid (gridW in pure coords)
			drawBar(gridW-barWidth, gridW, 0, mcfg.PM10Green, colorGreenZone)
			drawBar(gridW-barWidth, gridW, mcfg.PM10Green, mcfg.PM10Yellow, colorYellowZone)
			drawBar(gridW-barWidth, gridW, mcfg.PM10Yellow, math.MaxFloat64, colorRedZone)

			dashWidth := chartStrokeWidth * chartDashWidthCoef
			dashPattern := chartDashPattern

			drawThreshold := func(val float64, color charts.Color, isLeft bool) {
				if val < yMin || val > yMax {
					return
				}
				y := yFunc(val)
				var tx1, tx2 float64
				if isLeft {
					tx1 = 0.0
					tx2 = gridW - barWidth*chartThresholdPaddingCoef
			} else {
				tx1 = barWidth * chartThresholdPaddingCoef
				tx2 = gridW
			}
				pureCp.DashedLineStroke([]charts.Point{{X: int(tx1), Y: int(y)}, {X: int(tx2), Y: int(y)}}, color, dashWidth, dashPattern)
			}

			// 5. Draw intersection dots where data crosses thresholds
			drawDots := func(seriesData []float64, threshold float64, color charts.Color) {
				if len(seriesData) < 2 {
					return
				}
				yT := yFunc(threshold)
				for i := 0; i < len(seriesData)-1; i++ {
					v1 := seriesData[i]
					v2 := seriesData[i+1]
					// Check crossing
					if (v1 <= threshold && v2 >= threshold) || (v1 >= threshold && v2 <= threshold) {
						if v1 == v2 {
							continue
						}
						t := (threshold - v1) / (v2 - v1)
						x1 := float64(i) / float64(len(seriesData)-1) * gridW
						x2 := float64(i+1) / float64(len(seriesData)-1) * gridW
						x := x1 + t*(x2-x1)
						// Draw inner white dot inside a larger colored dot
						pureCp.Circle(chartStrokeWidth*chartDotLargeCoef, int(x), int(yT), color, color, 0)
						if !isAQI {
							pureCp.Circle(chartStrokeWidth*chartDotSmallCoef, int(x), int(yT), charts.ColorWhite, charts.ColorWhite, 0)
						}
					}
				}
			}

			// Draw PM10 thresholds and dots
			drawThreshold(mcfg.PM25Green, colorSeriesRed, true)
			drawDots(data[0], mcfg.PM25Green, colorSeriesRed)
			drawThreshold(mcfg.PM25Yellow, colorSeriesRed, true)
			drawDots(data[0], mcfg.PM25Yellow, colorSeriesRed)

			// Draw PM10 thresholds and dots (with small offset if identical to PM10)
			g10 := mcfg.PM10Green
			if g10 == mcfg.PM25Green {
				g10 -= (yMax - yMin) / plotHeight
			}
			drawThreshold(g10, colorSeriesBlue, false)
			drawDots(data[1], g10, colorSeriesBlue)

			y10 := mcfg.PM10Yellow
			if y10 == mcfg.PM25Yellow {
				y10 -= (yMax - yMin) / plotHeight
			}
			drawThreshold(y10, colorSeriesBlue, false)
			drawDots(data[1], y10, colorSeriesBlue)
		}

		if isAQI {
			cp := p.Child(charts.PainterPaddingOption(opt.Padding))
			yAxisWidth := calcYAxisWidth(chartFontSize, yMin, yMax, isAQI)
			xAxisHeight := int(chartFontSize * chartXAxisHeightCoef)
			titleHeight := int(chartFontSize * chartTitleHeightCoef)

			pureCp := cp.Child(charts.PainterPaddingOption(charts.Box{
				Left:   yAxisWidth,
				Bottom: xAxisHeight,
				Top:    titleHeight,
			}))

			gridW := float64(pureCp.Width())
			gridH := float64(pureCp.Height())
			yAxisBottom := gridH
			yAxisTop := 0.0
			plotHeight := yAxisBottom - yAxisTop

			yFunc := func(val float64) float64 {
				y := yAxisBottom - (val-yMin)/(yMax-yMin)*plotHeight
				if y < yAxisTop {
					y = yAxisTop
				}
				if y > yAxisBottom {
					y = yAxisBottom
				}
				return y
			}

			breakpoints := sensor.IndexPointsEU
			if mcfg.AQIStandard == "US" {
				breakpoints = sensor.IndexPointsUS
			}

			colors := []charts.Color{
				colorAQIGood, colorAQIModerate, colorAQISlightly,
				colorAQIUnhealthy, colorAQIVery, colorAQIHazardous, colorAQIHazardous,
			}
			if mcfg.AQIStandard == "EU" {
				colors = []charts.Color{
					colorAQILightBlue, colorAQIGood, colorAQIModerate,
					colorAQISlightly, colorAQIUnhealthy, colorAQIHazardous,
				}
			}

			// Draw background zones
			for i := 0; i < len(breakpoints)-1; i++ {
				low := breakpoints[i]
				high := breakpoints[i+1]
				if i == len(breakpoints)-2 && mcfg.AQIStandard == "US" {
					high = 500 // Cap for US
				}

				c := colors[i]
				c.A = 51 // ~20% opacity (255 * 0.2)

				pyBottom := yFunc(math.Max(low, yMin))
				pyTop := yFunc(math.Min(high, yMax))

				if pyBottom > pyTop {
					pureCp.FillArea([]charts.Point{
						{X: 0, Y: int(pyBottom)},
						{X: int(gridW), Y: int(pyBottom)},
						{X: int(gridW), Y: int(pyTop)},
						{X: 0, Y: int(pyTop)},
						{X: 0, Y: int(pyBottom)},
					}, c)
				}
			}

			// Draw dashed lines
			// 5. Draw intersection dots where data crosses thresholds
			drawDots := func(seriesData []float64, threshold float64, color charts.Color) {
				if len(seriesData) < 2 {
					return
				}
				yT := yFunc(threshold)
				for i := 0; i < len(seriesData)-1; i++ {
					v1 := seriesData[i]
					v2 := seriesData[i+1]
					if (v1 <= threshold && v2 >= threshold) || (v1 >= threshold && v2 <= threshold) {
						if v1 == v2 {
							continue
						}
						t := (threshold - v1) / (v2 - v1)
						x1 := float64(i) / float64(len(seriesData)-1) * gridW
						x2 := float64(i+1) / float64(len(seriesData)-1) * gridW
						x := x1 + t*(x2-x1)
						pureCp.Circle(chartStrokeWidth*2.4, int(x), int(yT), color, color, 0)

					}
				}
			}

			dashWidth := chartStrokeWidth * chartDashWidthCoef
			dashPattern := chartDashPattern
			for i := 1; i < len(breakpoints); i++ {
				val := breakpoints[i]
				if val < yMin || val > yMax {
					continue
				}
				y := yFunc(val)
				pureCp.DashedLineStroke([]charts.Point{{X: 0, Y: int(y)}, {X: int(gridW), Y: int(y)}}, colors[i-1], dashWidth, dashPattern)
				drawDots(data[0], val, colorSeriesGrey)
			}
		}

		return p.Bytes()
	}

	switch chartType {
	case "pm":
		return buildChart(pmTitle, b.T(chatID, "chart_unit_pm"),
			[]string{"PM2.5", "PM10"},
			[][]float64{pm25Values, pm10Values}, true)
	case "temp":
		if len(tempValues) > 0 {
			return buildChart(b.T(chatID, "msg_temp"), b.unitTempLabel(chatID),
				[]string{b.T(chatID, "msg_temp"), b.T(chatID, "msg_dew_point")},
				[][]float64{tempValues, dewPointValues}, false)
		}
	case "hum":
		if len(humValues) > 0 {
			return buildChart(b.T(chatID, "msg_hum"), "%", []string{b.T(chatID, "msg_hum")}, [][]float64{humValues}, false)
		}
	case "press":
		if len(pressValues) > 0 {
			return buildChart(b.T(chatID, "msg_press"), b.unitPressLabel(chatID), []string{b.T(chatID, "msg_press")}, [][]float64{pressValues}, false)
		}
	case "aqi":
		if len(aqiValues) > 0 {
			return buildChart(b.T(chatID, "btn_chart_aqi", ""), mcfg.AQIStandard, []string{"AQI"}, [][]float64{aqiValues}, true)
		}
	}

	return nil, nil
}

func CalcDewPoint(t, rh float64) float64 {
	if rh == 0 {
		return charts.GetNullValue()
	}
	gamma := math.Log(rh/100.0) + (magnusB*t)/(magnusC+t)
	return (magnusC * gamma) / (magnusB - gamma)
}
