package tgbot

import (
	"fmt"
	"sort"
	"time"

	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/go-analyze/charts"
)

// generateCharts produces a slice of PNG buffers containing PM, Temperature,
// Humidity, and Pressure charts based on the provided measurement history.
func generateCharts(b *Bot, chatID int64, hist []monitor.Measurement, pm10Thresh, pm25Thresh float64, chartWidth, chartHeight int, chartFontSize float64) ([][]byte, error) {
	if len(hist) == 0 {
		return nil, nil
	}

	var labels []string
	var pm10Values, pm25Values []float64
	var tempValues, humValues, pressValues []float64

	for _, m := range hist {
		labels = append(labels, m.Timestamp.Local().Format("15:04"))
		pm10Values = append(pm10Values, m.PM10)
		pm25Values = append(pm25Values, m.PM25)
		if m.Temperature != 0 {
			tempValues = append(tempValues, b.convertTemp(m.Temperature, chatID))
		}
		if m.Humidity != 0 {
			humValues = append(humValues, m.Humidity)
		}
		if m.Pressure != 0 {
			pressValues = append(pressValues, b.convertPress(m.Pressure, chatID))
		}
	}

	const strokeWidth = 3.0
	const smoothing = 0.4

	formatter := func(f float64) string {
		return fmt.Sprintf("%.1f", f)
	}

	hideSplit := charts.Ptr(false)

	pmTitle := b.T(chatID, "chart_pm_title")

	buildChart := func(title, yAxisName string, seriesNames []string, data [][]float64) ([]byte, error) {
		opt := charts.NewLineChartOptionWithData(data)
		opt.Title = charts.TitleOption{
			Text:      fmt.Sprintf("%s (%s)", title, yAxisName),
			FontStyle: charts.FontStyle{FontSize: chartFontSize * 1.2},
		}
		opt.XAxis = charts.XAxisOption{
			Labels:         labels,
			LabelFontStyle: charts.FontStyle{FontSize: chartFontSize},
		}

		opt.YAxis = []charts.YAxisOption{
			{
				// Left axis: hide lines to prevent double grid
				SplitLineShow: hideSplit,
				SpineLineShow: hideSplit,
				Show:          hideSplit, // Hide completely
			},
			{
				// Right axis
				Position:       charts.PositionRight,
				ValueFormatter: formatter,
				LabelFontStyle: charts.FontStyle{FontSize: chartFontSize},
			},
		}

		opt.Legend = charts.LegendOption{
			FontStyle: charts.FontStyle{FontSize: chartFontSize},
		}

		opt.LineStrokeWidth = strokeWidth
		opt.StrokeSmoothingTension = smoothing

		theme := charts.GetDefaultTheme()
		pm10Color := theme.GetSeriesColor(0)
		pm25Color := theme.GetSeriesColor(1)

		for i, name := range seriesNames {
			opt.SeriesList[i].Name = name
			opt.SeriesList[i].YAxisIndex = 1 // Use right Y axis

			// Special styling for threshold lines in PM chart
			if title == pmTitle && i >= 2 {
				color := pm10Color
				if i == 3 {
					color = pm25Color
				}
				opt.SeriesList[i].Symbol = charts.SymbolNone
				opt.SeriesList[i].TrendLine = []charts.SeriesTrendLine{
					{
						Type:            charts.SeriesTrendTypeLinear,
						LineColor:       color,
						LineStrokeWidth: strokeWidth * 0.7,
						DashedLine:      charts.Ptr(true),
					},
				}
			}
		}

		if title == pmTitle {
			opt.Theme = theme.WithSeriesColors([]charts.Color{
				pm10Color,
				pm25Color,
				{}, // Transparent for Threshold PM10
				{}, // Transparent for Threshold PM2.5
			})
		}

		p := charts.NewPainter(charts.PainterOptions{
			Width:  chartWidth,
			Height: chartHeight,
		})
		if err := p.LineChart(opt); err != nil {
			return nil, err
		}
		return p.Bytes()
	}

	var buffers [][]byte

	pm10ThreshVals := make([]float64, len(hist))
	pm25ThreshVals := make([]float64, len(hist))
	for i := range hist {
		pm10ThreshVals[i] = pm10Thresh
		pm25ThreshVals[i] = pm25Thresh
	}
	pmBuf, err := buildChart(pmTitle, b.T(chatID, "chart_unit_pm"),
		[]string{"PM10", "PM2.5", "", ""},
		[][]float64{pm10Values, pm25Values, pm10ThreshVals, pm25ThreshVals})
	if err != nil {
		return nil, err
	}
	buffers = append(buffers, pmBuf)

	if len(tempValues) > 0 {
		buf, err := buildChart(b.T(chatID, "msg_temp"), b.unitTempLabel(chatID), []string{b.T(chatID, "msg_temp")}, [][]float64{tempValues})
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	if len(humValues) > 0 {
		buf, err := buildChart(b.T(chatID, "msg_hum"), "%", []string{b.T(chatID, "msg_hum")}, [][]float64{humValues})
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	if len(pressValues) > 0 {
		buf, err := buildChart(b.T(chatID, "msg_press"), b.unitPressLabel(chatID), []string{b.T(chatID, "msg_press")}, [][]float64{pressValues})
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	return buffers, nil
}


// generateSingleChart produces a single PNG buffer containing the requested chart type
// for the last 24 hours.
func generateSingleChart(b *Bot, chatID int64, hist []monitor.Measurement, chartType string, pm10Thresh, pm25Thresh float64, chartWidth, chartHeight int, chartFontSize float64) ([]byte, error) {
	if len(hist) == 0 {
		return nil, nil
	}

	filteredHist := hist

	// Resample to 15-minute intervals for better chart readability and stable labels
	window := 15 * time.Minute
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
	var tempValues, humValues, pressValues []float64

	lastHour := -1
	for _, m := range filteredHist {
		local := m.Timestamp.Local()
		label := ""
		if local.Hour() != lastHour {
			label = local.Format("15:00")
			lastHour = local.Hour()
		}
		labels = append(labels, label)

		switch chartType {
		case "pm":
			pm10Values = append(pm10Values, m.PM10)
			pm25Values = append(pm25Values, m.PM25)
		case "temp":
			tempValues = append(tempValues, b.convertTemp(m.Temperature, chatID))
		case "hum":
			humValues = append(humValues, m.Humidity)
		case "press":
			pressValues = append(pressValues, b.convertPress(m.Pressure, chatID))
		}
	}

	const strokeWidth = 3.0
	const smoothing = 0.4

	formatter := func(f float64) string {
		return fmt.Sprintf("%.1f", f)
	}

	hideSplit := charts.Ptr(false)

	pmTitle := b.T(chatID, "chart_pm_title")

	buildChart := func(title, yAxisName string, seriesNames []string, data [][]float64) ([]byte, error) {
		opt := charts.NewLineChartOptionWithData(data)
		opt.Title = charts.TitleOption{
			Text:      fmt.Sprintf("%s (%s)", title, yAxisName),
			FontStyle: charts.FontStyle{FontSize: chartFontSize * 1.2},
		}
		opt.XAxis = charts.XAxisOption{
			Labels:         labels,
			LabelFontStyle: charts.FontStyle{FontSize: chartFontSize},
		}

		opt.YAxis = []charts.YAxisOption{
			{
				SplitLineShow: hideSplit,
				SpineLineShow: hideSplit,
				Show:          hideSplit,
			},
			{
				Position:       charts.PositionRight,
				ValueFormatter: formatter,
				LabelFontStyle: charts.FontStyle{FontSize: chartFontSize},
			},
		}

		opt.Legend = charts.LegendOption{
			FontStyle: charts.FontStyle{FontSize: chartFontSize},
		}

		opt.LineStrokeWidth = strokeWidth
		opt.StrokeSmoothingTension = smoothing

		theme := charts.GetDefaultTheme()
		pm10Color := theme.GetSeriesColor(0)
		pm25Color := theme.GetSeriesColor(1)

		for i, name := range seriesNames {
			opt.SeriesList[i].Name = name
			opt.SeriesList[i].YAxisIndex = 1

			if title == pmTitle && i >= 2 {
				color := pm10Color
				if i == 3 {
					color = pm25Color
				}
				opt.SeriesList[i].Symbol = charts.SymbolNone
				opt.SeriesList[i].TrendLine = []charts.SeriesTrendLine{
					{
						Type:            charts.SeriesTrendTypeLinear,
						LineColor:       color,
						LineStrokeWidth: strokeWidth * 0.7,
						DashedLine:      charts.Ptr(true),
					},
				}
			}
		}

		if title == pmTitle {
			opt.Theme = theme.WithSeriesColors([]charts.Color{
				pm10Color,
				pm25Color,
				{},
				{},
			})
		}

		p := charts.NewPainter(charts.PainterOptions{
			Width:  chartWidth,
			Height: chartHeight,
		})
		if err := p.LineChart(opt); err != nil {
			return nil, err
		}
		return p.Bytes()
	}

	switch chartType {
	case "pm":
		pm10ThreshVals := make([]float64, len(filteredHist))
		pm25ThreshVals := make([]float64, len(filteredHist))
		for i := range filteredHist {
			pm10ThreshVals[i] = pm10Thresh
			pm25ThreshVals[i] = pm25Thresh
		}
		return buildChart(pmTitle, b.T(chatID, "chart_unit_pm"),
			[]string{"PM10", "PM2.5", "", ""},
			[][]float64{pm10Values, pm25Values, pm10ThreshVals, pm25ThreshVals})
	case "temp":
		if len(tempValues) > 0 {
			return buildChart(b.T(chatID, "msg_temp"), b.unitTempLabel(chatID), []string{b.T(chatID, "msg_temp")}, [][]float64{tempValues})
		}
	case "hum":
		if len(humValues) > 0 {
			return buildChart(b.T(chatID, "msg_hum"), "%", []string{b.T(chatID, "msg_hum")}, [][]float64{humValues})
		}
	case "press":
		if len(pressValues) > 0 {
			return buildChart(b.T(chatID, "msg_press"), b.unitPressLabel(chatID), []string{b.T(chatID, "msg_press")}, [][]float64{pressValues})
		}
	}

	return nil, nil
}
