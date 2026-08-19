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
	history  []*Action    // newest-first slice, max maxHistorySize entries
	current  *Action      // action being executed right now
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

// UpdateConfig refreshes spec-derived fields without touching live sensor state.
func (p *pipeline) UpdateConfig(info device.ValveInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.info.Config = info.Config
	p.info.FriendlyName = info.FriendlyName
	p.info.ShortAddr = info.ShortAddr
	p.info.BridgeName = info.BridgeName
	p.info.BridgeHost = info.BridgeHost
	p.info.BridgePort = info.BridgePort
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
	// The keep-closed safety ping runs on its own goroutine. Sharing one select
	// with the action loop meant a slow action starved it for exactly as long
	// as the action was stuck: with retryCount × commandTimeout able to reach
	// tens of minutes, the safety net went silent precisely when it mattered.
	go p.keepClosedLoop(ctx)

	for {
		select {
		case a := <-p.actionCh:
			p.execute(ctx, a)

		case <-ctx.Done():
			if p.getState() == device.ValveStateOpen {
				info := p.GetInfo()
				closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				p.execute(closeCtx, newAction(info.Name, info.Namespace, ActionTypeClose, 0))
				cancel()
			}
			return
		}
	}
}

// keepClosedLoop fires the safety ping on its own schedule, independent of
// whatever the action pipeline is doing.
func (p *pipeline) keepClosedLoop(ctx context.Context) {
	interval := p.config().KeepClosedInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.keepClosedPing(ctx)
			// Re-read config and reset the ticker only if the interval changed.
			if cur := p.config().KeepClosedInterval; cur > 0 && cur != interval {
				interval = cur
				ticker.Reset(interval)
			}

		case <-ctx.Done():
			return
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
			if !power {
				p.markCloseUnconfirmed()
			}
		}
		return
	}

	// After a confirmed close, do an advisory ZbStatus3 check to catch valves that
	// acknowledge the command but remain physically open.
	if !power {
		info := p.GetInfo()
		go p.checkClosedWithStatus3(info.BridgeName, info.CommandTarget())
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
		closeErr := p.attemptWithRetry(closeCtx, closeAction, false, cfg.RetryCount, cfg.CommandTimeout)
		cancel2()

		info := p.GetInfo()
		go p.checkClosedWithStatus3(info.BridgeName, info.CommandTarget())

		closedAt := time.Now().UTC()
		a.ClosedAt = &closedAt

		// A timed open whose close was never confirmed is not a success.
		// Reporting it as fulfilled is how a valve that is physically still
		// open ends up with status.valveState "closed" and nothing anywhere
		// saying otherwise.
		if closeErr != nil {
			a.State = ActionStateFailed
			a.FailedAt = &closedAt
			a.ErrorMessage = fmt.Sprintf("timed open expired but close was not confirmed: %v", closeErr)
			p.metrics.ActionsTotal.WithLabelValues(
				a.ValveName, a.Namespace, string(a.Type), "close_unconfirmed").Inc()
			p.log.Error("timed open expired but close was not confirmed — valve may still be open",
				"actionID", a.ID, "device", info.CommandTarget(), "err", closeErr)
			p.markCloseUnconfirmed()
		} else {
			a.State = ActionStateFulfilled
		}
	}

	p.updateActionID("")
}

// attemptWithRetry sends the command and waits for device confirmation, retrying up to retryCount times.
// For close actions the command is sent CloseRepeatCount times per attempt to improve reliability.
func (p *pipeline) attemptWithRetry(ctx context.Context, a *Action, power bool, retryCount int, timeout time.Duration) error {
	a.State = ActionStateSending

	for attempt := 0; attempt < retryCount; attempt++ {
		if attempt > 0 {
			p.metrics.ActionRetries.WithLabelValues(a.ValveName, a.Namespace, string(a.Type)).Inc()
			p.log.Info("retrying command", "actionID", a.ID, "attempt", attempt+1)
		}

		// Re-read on every attempt rather than snapshotting once before the
		// loop. A rename, a bridge change or a config change must take effect
		// on the next attempt instead of being ignored until the entire retry
		// budget — up to retryCount × timeout — has been burnt through.
		info := p.GetInfo()
		target := info.CommandTarget()

		sendCount := 1
		if !power {
			sendCount = info.Config.CloseRepeatCount
			if sendCount < 1 {
				sendCount = 1
			}
		}

		a.Attempts = attempt + 1
		a.LastAttempt = time.Now().UTC()

		// Register waiter before the first publish to avoid missing a fast confirmation.
		expectedPower := 0
		if power {
			expectedPower = 1
		}
		waitCtx, waitCancel := context.WithTimeout(ctx, timeout)
		waitCh := waitForPower(waitCtx, target, expectedPower, p.mqttMgr.Dispatcher())

		a.State = ActionStateWaitingConfirmation

		published := false
		for i := 0; i < sendCount; i++ {
			if i > 0 {
				select {
				case <-time.After(500 * time.Millisecond):
				case <-waitCtx.Done():
				}
			}
			if err := p.mqttMgr.SendZbSend(ctx, info.BridgeName, target, power); err != nil {
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

// keepClosedPing publishes an unconditional OFF as a safety net.
//
// It deliberately bypasses the action pipeline: it claims no action slot,
// touches neither p.current nor p.cancelFn, and so can still fire while another
// action is stuck retrying — which is exactly the situation it exists for.
// markCloseUnconfirmed records that a close command was never acknowledged, so
// the valve's position is no longer known. Only a recorded "closed" is
// downgraded: a recorded "open" is both accurate and the safer thing to report
// when a close has just failed.
func (p *pipeline) markCloseUnconfirmed() {
	p.mu.Lock()
	if p.info.State != device.ValveStateClosed {
		p.mu.Unlock()
		return
	}
	p.info.State = device.ValveStateUnknown
	name := p.info.Name
	ns := p.info.Namespace
	p.mu.Unlock()

	p.log.Warn("close was not confirmed, valve position is now unknown", "valve", name)
	p.metrics.ValveState.WithLabelValues(name, ns).Set(-1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.writer.UpdateValveStatus(ctx, name, ns, device.ValveStateUnknown, ""); err != nil {
		p.log.Warn("failed to update K8s valve status", "err", err)
	}
}

func (p *pipeline) keepClosedPing(ctx context.Context) {
	p.mu.RLock()
	current := p.current
	info := p.info
	p.mu.RUnlock()

	// Suppress only while an action that deliberately holds the valve open is
	// in flight (the ActionStateClosing wait phase of a timed open, where the
	// valve must stay open until the timer fires). A close or keep-close that
	// is still retrying must NOT suppress the ping. A completed open — no
	// current action but state==open — still gets the ping; that is the point.
	if current != nil && !current.IsTerminal() && current.HoldsValveOpen() {
		return
	}

	target := info.CommandTarget()
	p.log.Debug("sending keep-closed safety ping", "device", target)
	p.metrics.SafetyClosesTotal.WithLabelValues(info.Name, info.Namespace).Inc()

	timeout := info.Config.CommandTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// Register the waiter before publishing, so a device that answers
	// immediately is not missed.
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	waitCh := waitForPower(waitCtx, target, 0, p.mqttMgr.Dispatcher())

	sendCount := info.Config.CloseRepeatCount
	if sendCount < 1 {
		sendCount = 1
	}
	for i := 0; i < sendCount; i++ {
		if i > 0 {
			select {
			case <-time.After(500 * time.Millisecond):
			case <-waitCtx.Done():
			}
		}
		if err := p.mqttMgr.SendZbSend(ctx, info.BridgeName, target, false); err != nil {
			p.log.Warn("keep-closed ping publish failed", "device", target, "repeat", i+1, "err", err)
		}
	}

	// On confirmation, keep the recorded state honest — but only when no action
	// owns the state machine, so the ping never overwrites an in-flight
	// action's bookkeeping.
	if err := <-waitCh; err != nil {
		p.log.Warn("keep-closed ping was not confirmed by the device", "device", target)
		return
	}
	if cur := p.GetCurrent(); cur == nil || cur.IsTerminal() {
		p.applyDeviceState(false)
	}
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
