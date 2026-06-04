package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hauke-cloud/iot/valve-controller/internal/device"
	"github.com/hauke-cloud/iot/valve-controller/internal/metrics"
	"github.com/hauke-cloud/iot/valve-controller/internal/mqtt"
)

var (
	ErrValveNotFound = errors.New("valve not found")
	ErrValveDisabled = errors.New("valve is disabled")
)

// Scheduler manages per-valve pipelines and is the primary entry point for the REST API.
type Scheduler struct {
	log     *slog.Logger
	metrics *metrics.Metrics
	mqttMgr *mqtt.Manager
	writer  StatusWriter

	mu        sync.RWMutex
	pipelines map[string]*pipeline // key: "namespace/name"
	cancels   map[string]context.CancelFunc
}

func New(mgr *mqtt.Manager, writer StatusWriter, m *metrics.Metrics, log *slog.Logger) *Scheduler {
	return &Scheduler{
		log:       log,
		metrics:   m,
		mqttMgr:   mgr,
		writer:    writer,
		pipelines: make(map[string]*pipeline),
		cancels:   make(map[string]context.CancelFunc),
	}
}

// RegisterValve creates or updates the pipeline for the given valve.
// It is safe to call on every reconcile; existing pipelines are updated in-place.
func (s *Scheduler) RegisterValve(ctx context.Context, info device.ValveInfo) {
	key := info.Key()
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.pipelines[key]; ok {
		p.UpdateConfig(info)
		return
	}

	pipeCtx, cancel := context.WithCancel(ctx)
	p := newPipeline(info, s.mqttMgr, s.writer, s.metrics, s.log)
	s.pipelines[key] = p
	s.cancels[key] = cancel
	go p.run(pipeCtx)

	s.log.Info("valve pipeline started", "valve", key)
}

// DeregisterValve stops the pipeline for the valve and removes it.
func (s *Scheduler) DeregisterValve(key string) {
	s.mu.Lock()
	cancel, ok := s.cancels[key]
	if ok {
		delete(s.pipelines, key)
		delete(s.cancels, key)
	}
	s.mu.Unlock()

	if ok {
		cancel()
		s.log.Info("valve pipeline stopped", "valve", key)
	}
}

// Open enqueues an open action for the valve. duration=0 means indefinite (subject to MaxOpenDuration).
func (s *Scheduler) Open(valveKey string, duration time.Duration) (*Action, error) {
	p, err := s.getPipeline(valveKey)
	if err != nil {
		return nil, err
	}
	info := p.GetInfo()
	if info.Config.Disabled {
		return nil, ErrValveDisabled
	}
	t := ActionTypeOpen
	if duration > 0 {
		t = ActionTypeOpenTimed
	}
	a := newAction(info.Name, info.Namespace, t, duration)
	return p.Enqueue(a), nil
}

// Close enqueues a close action for the valve.
func (s *Scheduler) Close(valveKey string) (*Action, error) {
	p, err := s.getPipeline(valveKey)
	if err != nil {
		return nil, err
	}
	info := p.GetInfo()
	a := newAction(info.Name, info.Namespace, ActionTypeClose, 0)
	return p.Enqueue(a), nil
}

// GetAction looks up an action by ID across all pipelines.
func (s *Scheduler) GetAction(valveKey, actionID string) (*Action, error) {
	p, err := s.getPipeline(valveKey)
	if err != nil {
		return nil, err
	}
	if cur := p.GetCurrent(); cur != nil && cur.ID == actionID {
		return cur, nil
	}
	for _, a := range p.GetHistory() {
		if a.ID == actionID {
			return a, nil
		}
	}
	return nil, fmt.Errorf("action %q not found", actionID)
}

// ListActions returns the current + historical actions for a valve.
func (s *Scheduler) ListActions(valveKey string) ([]*Action, error) {
	p, err := s.getPipeline(valveKey)
	if err != nil {
		return nil, err
	}
	history := p.GetHistory()
	if cur := p.GetCurrent(); cur != nil {
		return append([]*Action{cur}, history...), nil
	}
	return history, nil
}

// GetValveInfo returns the live ValveInfo for a valve.
func (s *Scheduler) GetValveInfo(valveKey string) (device.ValveInfo, error) {
	p, err := s.getPipeline(valveKey)
	if err != nil {
		return device.ValveInfo{}, err
	}
	return p.GetInfo(), nil
}

// FindKey returns the scheduler key for a valve given only its name,
// by scanning all registered valves. Returns an error if none or multiple match.
func (s *Scheduler) FindKey(name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var matches []string
	for key := range s.pipelines {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) == 2 && parts[1] == name {
			matches = append(matches, key)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%w: %s", ErrValveNotFound, name)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous valve name %q: found in namespaces %v — use namespace/name format", name, matches)
	}
}

// ListValves returns all registered valve keys.
func (s *Scheduler) ListValves() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.pipelines))
	for k := range s.pipelines {
		keys = append(keys, k)
	}
	return keys
}

// UpdateSensorFields propagates MQTT sensor data into the valve's info and metrics.
func (s *Scheduler) UpdateSensorFields(valveKey string, ev mqtt.SensorEvent) {
	s.mu.RLock()
	p, ok := s.pipelines[valveKey]
	s.mu.RUnlock()
	if !ok {
		return
	}

	p.mu.Lock()
	if ev.LinkQuality != nil {
		p.info.LinkQuality = int32(*ev.LinkQuality)
	}
	if ev.BatteryPercentage != nil {
		p.info.BatteryPercentage = int32(*ev.BatteryPercentage)
	}
	if ev.DailyIrrigationVolume != nil {
		p.info.DailyIrrigationVolume = *ev.DailyIrrigationVolume
	}
	if ev.LastValveOpenDuration != nil {
		p.info.LastValveOpenDuration = *ev.LastValveOpenDuration
	}
	name := p.info.Name
	ns := p.info.Namespace
	lq := p.info.LinkQuality
	bat := p.info.BatteryPercentage
	vol := p.info.DailyIrrigationVolume
	dur := p.info.LastValveOpenDuration
	p.mu.Unlock()

	s.metrics.LinkQuality.WithLabelValues(name, ns).Set(float64(lq))
	s.metrics.BatteryPercentage.WithLabelValues(name, ns).Set(float64(bat))
	s.metrics.DailyIrrigationVolume.WithLabelValues(name, ns).Set(vol)
	s.metrics.LastOpenDuration.WithLabelValues(name, ns).Set(float64(dur) / 1000.0)
}

func (s *Scheduler) getPipeline(key string) (*pipeline, error) {
	s.mu.RLock()
	p, ok := s.pipelines[key]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrValveNotFound, key)
	}
	return p, nil
}
