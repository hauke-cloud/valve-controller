package mqtt

import "testing"

// The office bridge always publishes telemetry under the friendly name, while
// the pipeline addresses the device by its short address. Before Dispatch
// matched on both, every confirmation was dropped on the floor and close
// commands retried until the retry budget ran out.
func TestDispatch_MatchesSubscriberOnShortAddrWhenEventCarriesFriendlyName(t *testing.T) {
	d := NewDispatcher()

	var got []SensorEvent
	unsub := d.Subscribe("0xDA76", func(ev SensorEvent) { got = append(got, ev) })
	defer unsub()

	power := 0
	d.Dispatch(SensorEvent{
		BridgeName: "tasmota_office",
		DeviceName: "valve-center",
		ShortAddr:  "0xDA76",
		Power:      &power,
	})

	if len(got) != 1 {
		t.Fatalf("handler called %d times, want 1", len(got))
	}
	if got[0].DeviceName != "valve-center" {
		t.Errorf("DeviceName = %q, want %q", got[0].DeviceName, "valve-center")
	}
}

// The reverse direction must keep working: a subscriber that only knows the
// friendly name still receives events.
func TestDispatch_MatchesSubscriberOnFriendlyName(t *testing.T) {
	d := NewDispatcher()

	calls := 0
	unsub := d.Subscribe("valve-center", func(SensorEvent) { calls++ })
	defer unsub()

	d.Dispatch(SensorEvent{DeviceName: "valve-center", ShortAddr: "0xDA76"})

	if calls != 1 {
		t.Fatalf("handler called %d times, want 1", calls)
	}
}

// A handler registered under both identifiers of the same device must fire once
// per event, not once per matching key.
func TestDispatch_DoesNotDoubleDeliver(t *testing.T) {
	d := NewDispatcher()

	calls := 0
	unsub := d.Subscribe("0xDA76", func(SensorEvent) { calls++ })
	defer unsub()

	// The ghost entry on the other coordinator reports Name == Device.
	d.Dispatch(SensorEvent{DeviceName: "0xDA76", ShortAddr: "0xDA76"})

	if calls != 1 {
		t.Fatalf("handler called %d times, want 1", calls)
	}
}

func TestDispatch_ShortAddrMatchIsCaseInsensitive(t *testing.T) {
	d := NewDispatcher()

	calls := 0
	unsub := d.Subscribe("0xda76", func(SensorEvent) { calls++ })
	defer unsub()

	d.Dispatch(SensorEvent{DeviceName: "valve-center", ShortAddr: "0xDA76"})

	if calls != 1 {
		t.Fatalf("handler called %d times, want 1", calls)
	}
}

// Friendly names are case-sensitive on the bridge and must not be folded.
func TestDispatch_FriendlyNameMatchIsCaseSensitive(t *testing.T) {
	d := NewDispatcher()

	calls := 0
	unsub := d.Subscribe("Valve-Center", func(SensorEvent) { calls++ })
	defer unsub()

	d.Dispatch(SensorEvent{DeviceName: "valve-center"})

	if calls != 0 {
		t.Fatalf("handler called %d times, want 0", calls)
	}
}

func TestDispatch_UnsubscribeStopsDelivery(t *testing.T) {
	d := NewDispatcher()

	calls := 0
	unsub := d.Subscribe("0xDA76", func(SensorEvent) { calls++ })
	unsub()

	d.Dispatch(SensorEvent{DeviceName: "valve-center", ShortAddr: "0xDA76"})

	if calls != 0 {
		t.Fatalf("handler called %d times after unsubscribe, want 0", calls)
	}
	if len(d.handlers) != 0 {
		t.Errorf("handlers map still holds %d keys, want 0", len(d.handlers))
	}
}
