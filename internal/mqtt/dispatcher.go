package mqtt

import (
	"regexp"
	"strings"
	"sync"
)

// hexAddr matches a Zigbee short address as Tasmota writes it, e.g. "0xDA76".
var hexAddr = regexp.MustCompile(`^0[xX][0-9a-fA-F]+$`)

// dispatchKey canonicalises a device identifier. Short addresses are folded to
// upper case so a lower-case spec.shortAddr in a hand-written MQTTDevice still
// matches the upper-case form Tasmota puts on the wire. Friendly names are left
// verbatim — they are case-sensitive on the bridge.
func dispatchKey(s string) string {
	if hexAddr.MatchString(s) {
		return strings.ToUpper(s)
	}
	return s
}

// Dispatcher routes incoming sensor events to registered per-device handlers.
// Handlers are expected to be fast and non-blocking.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string][]subscription
	nextID   int
}

type subscription struct {
	id      int
	handler func(SensorEvent)
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		handlers: make(map[string][]subscription),
	}
}

// Subscribe registers a handler for events from the given device, identified by
// either its friendly name or its short address.
// Returns an unsubscribe function that must be called to avoid leaks.
func (d *Dispatcher) Subscribe(deviceName string, handler func(SensorEvent)) func() {
	key := dispatchKey(deviceName)

	d.mu.Lock()
	d.nextID++
	id := d.nextID
	d.handlers[key] = append(d.handlers[key], subscription{id: id, handler: handler})
	d.mu.Unlock()

	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		subs := d.handlers[key]
		for i, s := range subs {
			if s.id == id {
				d.handlers[key] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(d.handlers[key]) == 0 {
			delete(d.handlers, key)
		}
	}
}

// Dispatch delivers an event to every handler registered under either the
// device's friendly name or its short address.
//
// Matching on both matters: a device can be renamed on the bridge at any time,
// and a subscriber that registered under one identifier would otherwise stop
// receiving events the moment telemetry started arriving under the other —
// silently, while the events themselves kept flowing.
func (d *Dispatcher) Dispatch(ev SensorEvent) {
	var subs []subscription
	seen := make(map[int]bool)

	d.mu.RLock()
	for _, name := range [...]string{ev.DeviceName, ev.ShortAddr} {
		if name == "" {
			continue
		}
		for _, s := range d.handlers[dispatchKey(name)] {
			if seen[s.id] {
				continue
			}
			seen[s.id] = true
			subs = append(subs, s)
		}
	}
	d.mu.RUnlock()

	for _, s := range subs {
		s.handler(ev)
	}
}
