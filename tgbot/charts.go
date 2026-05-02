package tgbot

import (
	"fmt"

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
			tempValues = append(tempValues, m.Temperature)
		}
		if m.Humidity != 0 {
			humValues = append(humValues, m.Humidity)
		}
		if m.Pressure != 0 {
			pressValues = append(pressValues, m.Pressure*hPaToMmHg)
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
		buf, err := buildChart(b.T(chatID, "msg_temp"), "°C", []string{b.T(chatID, "msg_temp")}, [][]float64{tempValues})
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
		buf, err := buildChart(b.T(chatID, "msg_press"), b.T(chatID, "msg_unit_mmhg"), []string{b.T(chatID, "msg_press")}, [][]float64{pressValues})
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	return buffers, nil
}
