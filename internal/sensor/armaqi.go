// Package sensor defines data structures and parsing for incoming sensor
// payloads (JSON) received from ESP8266-based sensors.
// This file implements the parsing logic for ArmAQI sensor JSON payloads.
package sensor

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// parseArmAQI unmarshals the body as ArmAQI payload into SensorData.
func parseArmAQI(addr string, body []byte) (*SensorData, error) {
	data := &SensorData{
		ClientIP:   addr,
		DateTime:   time.Now().UTC(),
		ID:         uuid.New(),
		DeviceType: string(StandardArmAQI),
	}
	if err := json.Unmarshal(body, data); err != nil {
		return nil, fmt.Errorf("parsing error: %w", err)
	}
	for i := range data.Values {
		data.Values[i].ID = uuid.New()
		data.Values[i].SensorID = data.ID
	}
	return data, nil
}
