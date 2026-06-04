package mqtt

import "sync"

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

// Subscribe registers a handler for events from the given device name.
// Returns an unsubscribe function that must be called to avoid leaks.
func (d *Dispatcher) Subscribe(deviceName string, handler func(SensorEvent)) func() {
	d.mu.Lock()
	d.nextID++
	id := d.nextID
	d.handlers[deviceName] = append(d.handlers[deviceName], subscription{id: id, handler: handler})
	d.mu.Unlock()

	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		subs := d.handlers[deviceName]
		for i, s := range subs {
			if s.id == id {
				d.handlers[deviceName] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(d.handlers[deviceName]) == 0 {
			delete(d.handlers, deviceName)
		}
	}
}

// Dispatch delivers an event to all handlers registered for the device name.
func (d *Dispatcher) Dispatch(ev SensorEvent) {
	d.mu.RLock()
	subs := make([]subscription, len(d.handlers[ev.DeviceName]))
	copy(subs, d.handlers[ev.DeviceName])
	d.mu.RUnlock()

	for _, s := range subs {
		s.handler(ev)
	}
}
