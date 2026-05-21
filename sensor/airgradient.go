// Package sensor defines data structures and parsing for incoming sensor
// payloads (JSON) received from ESP8266-based sensors.
// This file implements the parsing and structure for AirGradient sensor JSON payloads.
package sensor

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AirGradientPayload represents the raw JSON structure sent by an AirGradient sensor.
type AirGradientPayload struct {
	Wifi        *float64 `json:"wifi"`
	Rco2        *float64 `json:"rco2"`
	Pm01        *float64 `json:"pm01"`
	Pm02        *float64 `json:"pm02"`
	Pm10        *float64 `json:"pm10"`
	Pm003Count  *float64 `json:"pm003Count"`
	Pm003Count2 *float64 `json:"pm003_count"`
	Pm03Count   *float64 `json:"pm03"`
	Atmp        *float64 `json:"atmp"`
	Rhum        *float64 `json:"rhum"`
	Tvoc        *float64 `json:"tvoc"`
	TvocIndex   *float64 `json:"tvocIndex"`
	Nox         *float64 `json:"nox"`
	NoxIndex    *float64 `json:"noxIndex"`
	SerialNo    string   `json:"serialno"`
	ChipID      string   `json:"chipid"`
	EspID       string   `json:"esp8266id"`
}

// parseAirGradient unmarshals the body as AirGradient payload and maps it to SensorData.
func parseAirGradient(addr string, body []byte) (*SensorData, error) {
	var payload AirGradientPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parsing AirGradient error: %w", err)
	}

	data := &SensorData{
		ClientIP:   addr,
		DateTime:   time.Now().UTC(),
		ID:         uuid.New(),
		DeviceType: string(StandardAirGradient),
	}

	deviceID := payload.SerialNo
	if deviceID == "" {
		deviceID = payload.ChipID
	}
	if deviceID == "" {
		deviceID = payload.EspID
	}
	if deviceID == "" {
		host := addr
		if h, _, err := net.SplitHostPort(addr); err == nil {
			host = h
		}
		deviceID = "airgradient_" + strings.ReplaceAll(host, ".", "_")
	}
	data.ParentID = deviceID

	if payload.Pm003Count != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "pm003Count",
			Value: fmt.Sprintf("%g", *payload.Pm003Count),
		})
	} else if payload.Pm003Count2 != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "pm003Count",
			Value: fmt.Sprintf("%g", *payload.Pm003Count2),
		})
	} else if payload.Pm03Count != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "pm003Count",
			Value: fmt.Sprintf("%g", *payload.Pm03Count),
		})
	}

	if payload.Pm01 != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "pm01",
			Value: fmt.Sprintf("%g", *payload.Pm01),
		})
	}
	if payload.Pm02 != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "pm02",
			Value: fmt.Sprintf("%g", *payload.Pm02),
		})
	}
	if payload.Pm10 != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "pm10",
			Value: fmt.Sprintf("%g", *payload.Pm10),
		})
	}
	if payload.Atmp != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "atmp",
			Value: fmt.Sprintf("%g", *payload.Atmp),
		})
	}
	if payload.Rhum != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "rhum",
			Value: fmt.Sprintf("%g", *payload.Rhum),
		})
	}
	if payload.Rco2 != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "rco2",
			Value: fmt.Sprintf("%g", *payload.Rco2),
		})
	}
	if payload.Tvoc != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "tvoc",
			Value: fmt.Sprintf("%g", *payload.Tvoc),
		})
	} else if payload.TvocIndex != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "tvoc",
			Value: fmt.Sprintf("%g", *payload.TvocIndex),
		})
	}
	if payload.Nox != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "nox",
			Value: fmt.Sprintf("%g", *payload.Nox),
		})
	} else if payload.NoxIndex != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "nox",
			Value: fmt.Sprintf("%g", *payload.NoxIndex),
		})
	}
	if payload.Wifi != nil {
		data.Values = append(data.Values, SensorValueType{
			Type:  "wifi",
			Value: fmt.Sprintf("%g", *payload.Wifi),
		})
	}

	for i := range data.Values {
		data.Values[i].ID = uuid.New()
		data.Values[i].SensorID = data.ID
	}

	return data, nil
}
