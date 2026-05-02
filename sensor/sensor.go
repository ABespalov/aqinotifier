// Package sensor defines data structures and parsing for incoming sensor
// payloads (JSON) received from ESP8266-based sensors. The package prepares
// SensorData for storage by filling runtime fields such as ClientIP,
// DateTime and generated UUIDs.
package sensor

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
)

// SensorValueType represents a single sensor reading (a name/value pair).
//
// Fields:
//   - ID:       locally generated UUID used as the primary key in storage;
//   - SensorID: UUID of the parent SensorData record (foreign key);
//   - Type:     the name/type of the measurement as provided by the device;
//   - Value:    the string value of the measurement.
type SensorValueType struct {
	ID       uuid.UUID `json:"-" db:"id;primary_key"`
	SensorID uuid.UUID `json:"-" db:"sid"`
	Type     string    `json:"value_type" db:"t"`
	Value    string    `json:"value" db:"v"`
}

// String returns a human-readable representation of the SensorValueType.
func (s SensorValueType) String() string {
	return fmt.Sprintf("    type: %s; value: %s\n", s.Type, s.Value)
}

// SensorData groups metadata and a list of sensor values reported by a
// single device. Fields are mapped to the JSON payload sent by ESP8266-based
// sensors (see struct tags) and additional runtime fields are filled by
// Parse (ClientIP, DateTime, generated ID).
//
// Fields:
//   - ID:       locally generated UUID used as the primary key in storage;
//   - ClientIP: IP address of the sending device;
//   - DateTime: timestamp (UTC) when the data was received;
//   - ParentID: UUID of the parent SensorData record (foreign key);
//   - Version:  software version of the sending device;
//   - Values:   slice of SensorValueType representing the reported readings.
type SensorData struct {
	ID       uuid.UUID         `json:"-" db:"id;primary_key"`
	ClientIP string            `json:"-" db:"ip"`
	DateTime time.Time         `json:"-" db:"utc"`
	ParentID string            `json:"esp8266id" db:"sid"`
	Version  string            `json:"software_version" db:"ver"`
	Values   []SensorValueType `json:"sensordatavalues"`
}

// String returns a multi-line human-readable representation of SensorData.
// It extracts the host portion from ClientIP when the address contains a
// port (e.g. "host:port"), so IPv6 with port is handled correctly.
func (sd SensorData) String() string {
	host := sd.ClientIP
	if h, _, err := net.SplitHostPort(sd.ClientIP); err == nil {
		host = h
	}

	s := fmt.Sprintf("  date: %s;\n  ip: %s;\n  values:\n",
		sd.DateTime.Format("2006/01/02 15:04:05"),
		host,
	)
	for _, v := range sd.Values {
		s += v.String()
	}
	return s
}

// Parse decodes the incoming JSON payload (body) into SensorData and fills
// runtime-specific fields.
//
// Parameters:
//   - addr: client network address (typically "host:port" as returned by
//     net.Conn.RemoteAddr().String()). This value is stored in
//     SensorData.ClientIP.
//   - body: raw JSON payload received from the device.
//
// Behavior:
//   - unmarshals JSON into SensorData;
//   - sets ClientIP, DateTime (UTC) and generates a new UUID for the
//     SensorData record;
//   - for each SensorValueType in Values, generates a unique ID and sets
//     ParentID to the parent SensorData ID (establishing the parent link).
//
// Returns the populated SensorData pointer or an error if JSON decoding fails.
func Parse(addr string, body []byte) (*SensorData, error) {
	var data SensorData
	err := json.Unmarshal(body, &data)
	if err != nil {
		return nil, fmt.Errorf("parsing error: %w", err)
	}
	data.ClientIP = addr
	data.DateTime = time.Now().UTC()
	data.ID = uuid.New()
	for i := range data.Values {
		// give each value its own UUID and set ParentID to the sensor data ID
		data.Values[i].ID = uuid.New()
		data.Values[i].SensorID = data.ID
	}
	return &data, nil
}
