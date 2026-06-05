package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hauke-cloud/iot/valve-controller/internal/metrics"
)

// Manager manages one BridgeClient per Tasmota MQTT bridge.
// Clients are created/updated/removed as MQTTBridge CRs change.
type Manager struct {
	log        *slog.Logger
	metrics    *metrics.Metrics
	dispatcher *Dispatcher
	hostname   string

	mu      sync.RWMutex
	bridges map[string]*BridgeClient // key: BridgeName
}

func NewManager(hostname string, m *metrics.Metrics, log *slog.Logger) *Manager {
	return &Manager{
		log:        log,
		metrics:    m,
		dispatcher: NewDispatcher(),
		hostname:   hostname,
		bridges:    make(map[string]*BridgeClient),
	}
}

// Dispatcher returns the shared event dispatcher so the scheduler can subscribe to device events.
func (mgr *Manager) Dispatcher() *Dispatcher {
	return mgr.dispatcher
}

// Upsert creates or replaces the MQTT client for a bridge.
// If the bridge already exists with the same host/port, this is a no-op.
func (mgr *Manager) Upsert(ctx context.Context, cfg BridgeConfig) error {
	cfg.Hostname = mgr.hostname

	mgr.mu.Lock()
	existing, ok := mgr.bridges[cfg.BridgeName]
	mgr.mu.Unlock()

	if ok {
		if existing.cfg.Host == cfg.Host && existing.cfg.Port == cfg.Port &&
			existing.cfg.Username == cfg.Username &&
			topicsEqual(existing.cfg.Topics, cfg.Topics) {
			return nil
		}
		existing.Disconnect()
	}

	bc := newBridgeClient(cfg, mgr.dispatcher, mgr.metrics, mgr.log)
	if err := bc.Connect(ctx); err != nil {
		return fmt.Errorf("bridge %s: %w", cfg.BridgeName, err)
	}

	mgr.mu.Lock()
	mgr.bridges[cfg.BridgeName] = bc
	mgr.mu.Unlock()

	mgr.log.Info("bridge client registered", "bridge", cfg.BridgeName, "host", cfg.Host)
	return nil
}

// Remove disconnects and removes the client for the given bridge.
func (mgr *Manager) Remove(bridgeName string) {
	mgr.mu.Lock()
	bc, ok := mgr.bridges[bridgeName]
	if ok {
		delete(mgr.bridges, bridgeName)
	}
	mgr.mu.Unlock()

	if ok {
		bc.Disconnect()
		mgr.log.Info("bridge client removed", "bridge", bridgeName)
	}
}

// SendZbStatus3 queries a device's state via ZbStatus3 on the given bridge.
func (mgr *Manager) SendZbStatus3(ctx context.Context, bridgeName, deviceName string, timeout time.Duration) (ZbStatus3ValveResult, error) {
	mgr.mu.RLock()
	bc, ok := mgr.bridges[bridgeName]
	mgr.mu.RUnlock()
	if !ok {
		return ZbStatus3ValveResult{}, fmt.Errorf("bridge %q not connected", bridgeName)
	}
	return bc.SendZbStatus3(ctx, deviceName, timeout)
}

// SendZbSend sends a ZbSend command on the bridge identified by bridgeName.
func (mgr *Manager) SendZbSend(ctx context.Context, bridgeName, deviceName string, power bool) error {
	mgr.mu.RLock()
	bc, ok := mgr.bridges[bridgeName]
	mgr.mu.RUnlock()
	if !ok {
		return fmt.Errorf("bridge %q not connected", bridgeName)
	}
	return bc.SendZbSend(ctx, deviceName, power)
}

// ConnectionStates returns a snapshot of per-bridge connection state for the readiness probe.
func (mgr *Manager) ConnectionStates() map[string]bool {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	out := make(map[string]bool, len(mgr.bridges))
	for name, bc := range mgr.bridges {
		out[name] = bc.IsConnected()
	}
	return out
}

func topicsEqual(a, b []TopicEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// StopAll disconnects all bridges gracefully.
func (mgr *Manager) StopAll() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	for _, bc := range mgr.bridges {
		bc.Disconnect()
	}
	mgr.bridges = make(map[string]*BridgeClient)
}
