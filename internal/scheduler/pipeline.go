package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hauke-cloud/iot/valve-controller/internal/device"
	"github.com/hauke-cloud/iot/valve-controller/internal/metrics"
	"github.com/hauke-cloud/iot/valve-controller/internal/mqtt"
)

const maxHistorySize = 100

// pipeline processes valve commands sequentially for a single valve.
// One goroutine runs the main loop; actions arrive via Enqueue.
type pipeline struct {
	log     *slog.Logger
	metrics *metrics.Metrics
	mqttMgr *mqtt.Manager
	writer  StatusWriter

	mu       sync.RWMutex
	info     device.ValveInfo
	cancelFn context.CancelFunc // cancels the currently executing action

	actionCh chan *Action // buffered 8, receives enqueued actions
	history  []*Action   // newest-first slice, max maxHistorySize entries
	current  *Action     // action being executed right now
}

// StatusWriter allows the pipeline to push K8s status updates.
type StatusWriter interface {
	UpdateValveStatus(ctx context.Context, name, namespace string, state device.ValveState, actionID string) error
}

func newPipeline(info device.ValveInfo, mgr *mqtt.Manager, w StatusWriter, m *metrics.Metrics, log *slog.Logger) *pipeline {
	return &pipeline{
		log:      log.With("valve", info.Key()),
		metrics:  m,
		mqttMgr:  mgr,
		writer:   w,
		info:     info,
		actionCh: make(chan *Action, 8),
	}
}

// UpdateConfig refreshes the valve spec without restarting the goroutine.
func (p *pipeline) UpdateConfig(info device.ValveInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.info = info
}

// Enqueue adds an action to the pipeline.
// If a close action arrives while an open is active, the active action is cancelled.
func (p *pipeline) Enqueue(a *Action) *Action {
	p.mu.Lock()
	if a.Type == ActionTypeClose && p.cancelFn != nil {
		p.cancelFn()
	}
	p.mu.Unlock()

	// Non-blocking enqueue; if the queue is full, discard the oldest pending item.
	select {
	case p.actionCh <- a:
	default:
		select {
		case <-p.actionCh:
		default:
		}
		p.actionCh <- a
	}
	return a
}

// GetCurrent returns a snapshot of the currently executing action, or nil when idle.
func (p *pipeline) GetCurrent() *Action {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

// GetHistory returns a copy of the completed action history, newest first.
func (p *pipeline) GetHistory() []*Action {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Action, len(p.history))
	copy(out, p.history)
	return out
}

// GetInfo returns a copy of the current ValveInfo.
func (p *pipeline) GetInfo() device.ValveInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.info
}

// run is the main pipeline goroutine. Must be started exactly once via go p.run(ctx).
func (p *pipeline) run(ctx context.Context) {
	cfg := p.config()
	lastInterval := cfg.KeepClosedInterval
	ticker := time.NewTicker(lastInterval)
	defer ticker.Stop()

	for {
		select {
		case a := <-p.actionCh:
			p.execute(ctx, a)

		case <-ticker.C:
			p.keepClosedPing(ctx)

		case <-ctx.Done():
			if p.getState() == device.ValveStateOpen {
				closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				p.execute(closeCtx, newAction(p.info.Name, p.info.Namespace, ActionTypeClose, 0))
				cancel()
			}
			return
		}

		// Re-read config and reset ticker only if the interval changed.
		cfg = p.config()
		if cfg.KeepClosedInterval != lastInterval && cfg.KeepClosedInterval > 0 {
			lastInterval = cfg.KeepClosedInterval
			ticker.Reset(lastInterval)
		}
	}
}

// execute runs one action through the send → confirm → (optional timer+close) cycle.
func (p *pipeline) execute(ctx context.Context, a *Action) {
	cfg := p.config()

	// Safety: upgrade indefinite open to timed open when MaxOpenDuration is set.
	if a.Type == ActionTypeOpen && cfg.MaxOpenDuration > 0 {
		a.Type = ActionTypeOpenTimed
		a.Duration = cfg.MaxOpenDuration
		p.log.Info("upgraded open to timed open (MaxOpenDuration)",
			"duration", cfg.MaxOpenDuration, "actionID", a.ID)
	}

	if cfg.Disabled && (a.Type == ActionTypeOpen || a.Type == ActionTypeOpenTimed) {
		a.State = ActionStateCancelled
		p.log.Info("valve disabled, open rejected", "actionID", a.ID)
		p.addToHistory(a)
		return
	}

	actionCtx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.cancelFn = cancel
	p.current = a
	p.mu.Unlock()

	defer func() {
		cancel()
		p.mu.Lock()
		p.cancelFn = nil
		p.current = nil
		p.mu.Unlock()
		if !a.IsInternalSafetyPing() {
			p.addToHistory(a)
		}
	}()

	power := a.Type != ActionTypeClose && a.Type != ActionTypeKeepClose
	if err := p.attemptWithRetry(actionCtx, a, power, cfg.RetryCount, cfg.CommandTimeout); err != nil {
		if a.State != ActionStateCancelled {
			a.State = ActionStateFailed
			now := time.Now().UTC()
			a.FailedAt = &now
			a.ErrorMessage = err.Error()
			if !a.IsInternalSafetyPing() {
				p.metrics.ActionsTotal.WithLabelValues(a.ValveName, a.Namespace, string(a.Type), "failed").Inc()
			}
			p.log.Warn("action failed", "actionID", a.ID, "type", a.Type, "err", err)
		}
		return
	}

	// After a confirmed close, do an advisory ZbStatus3 check to catch valves that
	// acknowledge the command but remain physically open.
	if !power {
		info := p.GetInfo()
		go p.checkClosedWithStatus3(info.BridgeName, info.FriendlyName)
	}

	now := time.Now().UTC()
	a.FulfilledAt = &now
	a.State = ActionStateFulfilled
	elapsed := now.Sub(a.RequestedAt).Seconds()

	if !a.IsInternalSafetyPing() {
		p.metrics.ActionsTotal.WithLabelValues(a.ValveName, a.Namespace, string(a.Type), "fulfilled").Inc()
		p.metrics.ActionDuration.WithLabelValues(a.ValveName, a.Namespace, string(a.Type)).Observe(elapsed)
		p.log.Info("action fulfilled", "actionID", a.ID, "type", a.Type, "elapsed_s", elapsed)
	}

	// For timed opens: wait out the duration then close the valve.
	if a.Type == ActionTypeOpenTimed {
		expires := now.Add(a.Duration)
		a.ExpiresAt = &expires
		a.State = ActionStateClosing
		p.updateActionID(a.ID)

		select {
		case <-time.After(a.Duration):
		case <-actionCtx.Done():
		}

		// Close regardless of cancellation — safety first.
		closeCtx, cancel2 := context.WithTimeout(context.Background(), 2*time.Minute)
		closeAction := newAction(a.ValveName, a.Namespace, ActionTypeClose, 0)
		_ = p.attemptWithRetry(closeCtx, closeAction, false, cfg.RetryCount, cfg.CommandTimeout)
		cancel2()

		info := p.GetInfo()
		go p.checkClosedWithStatus3(info.BridgeName, info.FriendlyName)

		closedAt := time.Now().UTC()
		a.ClosedAt = &closedAt
		a.State = ActionStateFulfilled
	}

	p.updateActionID("")
}

// attemptWithRetry sends the command and waits for device confirmation, retrying up to retryCount times.
// For close actions the command is sent CloseRepeatCount times per attempt to improve reliability.
func (p *pipeline) attemptWithRetry(ctx context.Context, a *Action, power bool, retryCount int, timeout time.Duration) error {
	a.State = ActionStateSending
	info := p.GetInfo()

	sendCount := 1
	if !power {
		sendCount = info.Config.CloseRepeatCount
		if sendCount < 1 {
			sendCount = 1
		}
	}

	for attempt := 0; attempt < retryCount; attempt++ {
		if attempt > 0 {
			p.metrics.ActionRetries.WithLabelValues(a.ValveName, a.Namespace, string(a.Type)).Inc()
			p.log.Info("retrying command", "actionID", a.ID, "attempt", attempt+1)
		}

		a.Attempts = attempt + 1
		a.LastAttempt = time.Now().UTC()

		// Register waiter before the first publish to avoid missing a fast confirmation.
		expectedPower := 0
		if power {
			expectedPower = 1
		}
		waitCtx, waitCancel := context.WithTimeout(ctx, timeout)
		waitCh := waitForPower(waitCtx, info.FriendlyName, expectedPower, p.mqttMgr.Dispatcher())

		a.State = ActionStateWaitingConfirmation

		published := false
		for i := 0; i < sendCount; i++ {
			if i > 0 {
				select {
				case <-time.After(500 * time.Millisecond):
				case <-waitCtx.Done():
				}
			}
			if err := p.mqttMgr.SendZbSend(ctx, info.BridgeName, info.FriendlyName, power); err != nil {
				p.log.Warn("ZbSend publish failed", "actionID", a.ID, "attempt", attempt+1, "repeat", i+1, "err", err)
			} else {
				published = true
			}
		}

		if !published {
			waitCancel()
			continue
		}

		err := <-waitCh
		waitCancel()
		if err == nil {
			p.applyDeviceState(power)
			return nil
		}
		if ctx.Err() != nil {
			a.State = ActionStateCancelled
			return ctx.Err()
		}
		p.log.Warn("confirmation timeout", "actionID", a.ID, "attempt", attempt+1)
	}

	return fmt.Errorf("no device confirmation after %d attempts", retryCount)
}

// applyDeviceState updates local state and propagates to K8s + metrics.
func (p *pipeline) applyDeviceState(open bool) {
	p.mu.Lock()
	now := time.Now().UTC()
	if open {
		p.info.State = device.ValveStateOpen
		p.info.LastOpenTime = &now
	} else {
		p.info.State = device.ValveStateClosed
		p.info.LastCloseTime = &now
	}
	name := p.info.Name
	ns := p.info.Namespace
	state := p.info.State
	p.mu.Unlock()

	stateVal := map[device.ValveState]float64{
		device.ValveStateOpen:    1,
		device.ValveStateClosed:  0,
		device.ValveStateUnknown: -1,
	}[state]
	p.metrics.ValveState.WithLabelValues(name, ns).Set(stateVal)

	updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	actionID := ""
	if c := p.GetCurrent(); c != nil {
		actionID = c.ID
	}
	if err := p.writer.UpdateValveStatus(updateCtx, name, ns, state, actionID); err != nil {
		p.log.Warn("failed to update K8s valve status", "err", err)
	}
}

func (p *pipeline) keepClosedPing(ctx context.Context) {
	p.mu.RLock()
	state := p.info.State
	current := p.current
	name := p.info.Name
	ns := p.info.Namespace
	p.mu.RUnlock()

	if current != nil && !current.IsTerminal() {
		return
	}
	if state == device.ValveStateOpen {
		return
	}

	p.log.Debug("sending keep-closed safety ping")
	p.metrics.SafetyClosesTotal.WithLabelValues(name, ns).Inc()
	p.execute(ctx, newAction(name, ns, ActionTypeKeepClose, 0))
}

func (p *pipeline) config() device.ValveConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.info.Config
}

func (p *pipeline) getState() device.ValveState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.info.State
}

func (p *pipeline) updateActionID(id string) {
	p.mu.Lock()
	p.info.CurrentActionID = id
	name := p.info.Name
	ns := p.info.Namespace
	state := p.info.State
	p.mu.Unlock()

	updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = p.writer.UpdateValveStatus(updateCtx, name, ns, state, id)
}

func (p *pipeline) addToHistory(a *Action) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.history = append([]*Action{a}, p.history...)
	if len(p.history) > maxHistorySize {
		p.history = p.history[:maxHistorySize]
	}
}

// checkClosedWithStatus3 polls ZbStatus3 two seconds after a close command was confirmed
// and logs a warning if the device reports it is still open. Advisory only — Tasmota sometimes
// reports closed even when the valve is physically open, but Power=1 here is a red flag.
func (p *pipeline) checkClosedWithStatus3(bridgeName, deviceName string) {
	time.Sleep(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := p.mqttMgr.SendZbStatus3(ctx, bridgeName, deviceName, 8*time.Second)
	if err != nil {
		p.log.Debug("ZbStatus3 check after close failed", "device", deviceName, "err", err)
		return
	}
	if r.Power != nil && *r.Power != 0 {
		p.log.Warn("ZbStatus3 reports valve still open after close command",
			"device", deviceName, "power", *r.Power)
	} else {
		p.log.Debug("ZbStatus3 confirms valve closed", "device", deviceName)
	}
}

// waitForPower subscribes to dispatcher events and returns a channel that receives nil
// when the device reports the expected power state, or ctx.Err() on timeout/cancel.
func waitForPower(ctx context.Context, deviceName string, expectedPower int, disp *mqtt.Dispatcher) <-chan error {
	ch := make(chan error, 1)
	done := make(chan struct{})

	var unsub func()
	unsub = disp.Subscribe(deviceName, func(ev mqtt.SensorEvent) {
		select {
		case <-done:
			return
		default:
		}
		if ev.Power != nil && *ev.Power == expectedPower {
			select {
			case ch <- nil:
				close(done)
				go unsub()
			default:
			}
		}
	})

	go func() {
		select {
		case <-ctx.Done():
			select {
			case <-done:
			default:
				close(done)
				unsub()
				ch <- ctx.Err()
			}
		case <-done:
		}
	}()

	return ch
}
