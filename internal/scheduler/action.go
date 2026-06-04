package scheduler

import (
	"crypto/rand"
	"fmt"
	"time"
)

// ActionType classifies a valve command.
type ActionType string

const (
	ActionTypeOpen      ActionType = "open"
	ActionTypeOpenTimed ActionType = "open_timed"
	ActionTypeClose     ActionType = "close"
	ActionTypeKeepClose ActionType = "keep_close" // internal safety ping
)

// ActionState tracks progress through the command pipeline.
type ActionState string

const (
	ActionStatePending             ActionState = "pending"
	ActionStateSending             ActionState = "sending"
	ActionStateWaitingConfirmation ActionState = "waiting_confirmation"
	ActionStateFulfilled           ActionState = "fulfilled"
	ActionStateClosing             ActionState = "closing"   // timed open expired → sending close
	ActionStateFailed              ActionState = "failed"
	ActionStateCancelled           ActionState = "cancelled"
)

// Action represents a single valve command in the pipeline.
type Action struct {
	ID           string
	ValveName    string
	Namespace    string
	Type         ActionType
	Duration     time.Duration // only for ActionTypeOpenTimed
	RequestedAt  time.Time
	State        ActionState
	Attempts     int
	LastAttempt  time.Time
	FulfilledAt  *time.Time
	ClosedAt     *time.Time
	ExpiresAt    *time.Time // set for timed opens after fulfillment
	FailedAt     *time.Time
	ErrorMessage string
}

func newAction(valveName, namespace string, t ActionType, duration time.Duration) *Action {
	return &Action{
		ID:          generateID(),
		ValveName:   valveName,
		Namespace:   namespace,
		Type:        t,
		Duration:    duration,
		RequestedAt: time.Now().UTC(),
		State:       ActionStatePending,
	}
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// IsTerminal reports whether the action has reached a final state.
func (a *Action) IsTerminal() bool {
	switch a.State {
	case ActionStateFulfilled, ActionStateFailed, ActionStateCancelled:
		return true
	}
	return false
}

// IsInternalSafetyPing reports whether this is an auto-generated keep-closed action.
func (a *Action) IsInternalSafetyPing() bool {
	return a.Type == ActionTypeKeepClose
}
