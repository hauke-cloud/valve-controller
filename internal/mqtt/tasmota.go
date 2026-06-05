package mqtt

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SensorEvent carries a parsed tele/<bridge>/SENSOR ZbReceived entry.
type SensorEvent struct {
	BridgeName            string
	DeviceName            string // friendly name or short address
	Power                 *int   // nil if field absent
	LinkQuality           *int
	BatteryPercentage     *int
	DailyIrrigationVolume *float64
	LastValveOpenDuration *int64
	IrrigationEndTime     *int64
}

// ResultEvent carries a parsed stat/<bridge>/RESULT payload.
type ResultEvent struct {
	BridgeName string
	ZbSendDone bool // true when {"ZbSend":"Done"}
}

// ParseSensorPayload parses a tele/<bridge>/SENSOR message.
// bridgeName is extracted from the topic by the caller.
func ParseSensorPayload(bridgeName string, payload []byte) ([]SensorEvent, error) {
	var wrapper struct {
		ZbReceived map[string]json.RawMessage `json:"ZbReceived"`
	}
	if err := json.Unmarshal(payload, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal sensor payload: %w", err)
	}
	if len(wrapper.ZbReceived) == 0 {
		return nil, nil
	}

	events := make([]SensorEvent, 0, len(wrapper.ZbReceived))
	for _, raw := range wrapper.ZbReceived {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			continue
		}
		ev := SensorEvent{BridgeName: bridgeName}
		if v, ok := fields["Name"]; ok {
			_ = json.Unmarshal(v, &ev.DeviceName)
		}
		if v, ok := fields["Power"]; ok {
			var p int
			if err := json.Unmarshal(v, &p); err == nil {
				ev.Power = &p
			}
		}
		if v, ok := fields["LinkQuality"]; ok {
			var lq int
			if err := json.Unmarshal(v, &lq); err == nil {
				ev.LinkQuality = &lq
			}
		}
		if v, ok := fields["BatteryPercentage"]; ok {
			var bp int
			if err := json.Unmarshal(v, &bp); err == nil {
				ev.BatteryPercentage = &bp
			}
		}
		if v, ok := fields["dailyIrrigationVolume"]; ok {
			var vol float64
			if err := json.Unmarshal(v, &vol); err == nil {
				ev.DailyIrrigationVolume = &vol
			}
		}
		if v, ok := fields["lastValveOpenDuration"]; ok {
			var dur int64
			if err := json.Unmarshal(v, &dur); err == nil {
				ev.LastValveOpenDuration = &dur
			}
		}
		if v, ok := fields["irrigationEndTime"]; ok {
			var t int64
			if err := json.Unmarshal(v, &t); err == nil {
				ev.IrrigationEndTime = &t
			}
		}
		if ev.DeviceName != "" {
			events = append(events, ev)
		}
	}
	return events, nil
}

// ParseResultPayload parses a stat/<bridge>/RESULT message.
func ParseResultPayload(bridgeName string, payload []byte) (*ResultEvent, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal result payload: %w", err)
	}
	ev := &ResultEvent{BridgeName: bridgeName}
	if v, ok := raw["ZbSend"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			ev.ZbSendDone = strings.EqualFold(s, "done")
		}
	}
	return ev, nil
}

// BridgeNameFromTopic extracts the bridge name from tele/<bridge>/SENSOR or stat/<bridge>/RESULT.
func BridgeNameFromTopic(topic string) string {
	parts := strings.SplitN(topic, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// ZbStatus3ValveResult holds the Power state from one device entry in a ZbStatus3 response.
type ZbStatus3ValveResult struct {
	DeviceName string
	Power      *int
}

// ParseZbStatus3ValvePower extracts device names and Power values from a stat/+/RESULT ZbStatus3 payload.
// Returns an empty slice (no error) when the payload contains no ZbStatus3 key.
func ParseZbStatus3ValvePower(payload []byte) ([]ZbStatus3ValveResult, error) {
	var wrapper struct {
		ZbStatus3 []struct {
			Name  string `json:"Name"`
			Power *int   `json:"Power"`
		} `json:"ZbStatus3"`
	}
	if err := json.Unmarshal(payload, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal ZbStatus3: %w", err)
	}
	out := make([]ZbStatus3ValveResult, 0, len(wrapper.ZbStatus3))
	for _, item := range wrapper.ZbStatus3 {
		if item.Name != "" {
			out = append(out, ZbStatus3ValveResult{DeviceName: item.Name, Power: item.Power})
		}
	}
	return out, nil
}

// ZbSendPayload builds the JSON payload for cmnd/<bridge>/ZbSend.
func ZbSendPayload(deviceName string, power bool) ([]byte, error) {
	powerVal := "OFF"
	if power {
		powerVal = "ON"
	}
	return json.Marshal(map[string]interface{}{
		"Device": deviceName,
		"Send":   map[string]string{"Power": powerVal},
	})
}
