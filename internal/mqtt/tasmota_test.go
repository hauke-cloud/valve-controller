package mqtt

import "testing"

// Payloads captured from the live office bridge.
func TestParseSensorPayload_ExtractsBothIdentifiers(t *testing.T) {
	payload := []byte(`{"ZbReceived":{"0xDA76":{"Device":"0xDA76","Name":"valve-center","Power":0,"Endpoint":1,"LinkQuality":120}}}`)

	events, err := ParseSensorPayload("tasmota_office", payload)
	if err != nil {
		t.Fatalf("ParseSensorPayload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	ev := events[0]
	if ev.DeviceName != "valve-center" {
		t.Errorf("DeviceName = %q, want %q", ev.DeviceName, "valve-center")
	}
	if ev.ShortAddr != "0xDA76" {
		t.Errorf("ShortAddr = %q, want %q", ev.ShortAddr, "0xDA76")
	}
	if ev.Power == nil || *ev.Power != 0 {
		t.Errorf("Power = %v, want 0", ev.Power)
	}
}

// An unnamed device reports no "Name" at all. It must still produce an event,
// addressable by short address.
func TestParseSensorPayload_KeepsEventWithoutFriendlyName(t *testing.T) {
	payload := []byte(`{"ZbReceived":{"0x0B6E":{"Device":"0x0B6E","Power":1,"Endpoint":1}}}`)

	events, err := ParseSensorPayload("tasmota_office", payload)
	if err != nil {
		t.Fatalf("ParseSensorPayload: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].ShortAddr != "0x0B6E" {
		t.Errorf("ShortAddr = %q, want %q", events[0].ShortAddr, "0x0B6E")
	}
	if events[0].DeviceName != "" {
		t.Errorf("DeviceName = %q, want empty", events[0].DeviceName)
	}
}

func TestParseZbStatus3ValvePower_ExtractsBothIdentifiers(t *testing.T) {
	payload := []byte(`{"ZbStatus3":[{"Device":"0xDA76","Name":"valve-center","IEEEAddr":"0xF4B3B1FFFE4E213B","ModelId":"SWV","Power":0,"Reachable":true}]}`)

	results, err := ParseZbStatus3ValvePower(payload)
	if err != nil {
		t.Fatalf("ParseZbStatus3ValvePower: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].DeviceName != "valve-center" || results[0].ShortAddr != "0xDA76" {
		t.Errorf("got (%q, %q), want (%q, %q)",
			results[0].DeviceName, results[0].ShortAddr, "valve-center", "0xDA76")
	}
	if results[0].Power == nil || *results[0].Power != 0 {
		t.Errorf("Power = %v, want 0", results[0].Power)
	}
}

// The stale ghost entry on the bedroom coordinator: no model, no endpoints, and
// a "Name" that is just the short address.
func TestParseZbStatus3ValvePower_GhostEntry(t *testing.T) {
	payload := []byte(`{"ZbStatus3":[{"Device":"0xDA76","Name":"0xDA76","IEEEAddr":"0xF4B3B1FFFE4E213B","Endpoints":[],"Config":[],"Reachable":true}]}`)

	results, err := ParseZbStatus3ValvePower(payload)
	if err != nil {
		t.Fatalf("ParseZbStatus3ValvePower: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Power != nil {
		t.Errorf("Power = %v, want nil (absent)", *results[0].Power)
	}
}

func TestParseZbStatus3ValvePower_NoZbStatus3Key(t *testing.T) {
	results, err := ParseZbStatus3ValvePower([]byte(`{"ZbSend":"Done"}`))
	if err != nil {
		t.Fatalf("ParseZbStatus3ValvePower: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}
