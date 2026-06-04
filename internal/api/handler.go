package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hauke-cloud/iot/valve-controller/internal/device"
	mqttclient "github.com/hauke-cloud/iot/valve-controller/internal/mqtt"
	"github.com/hauke-cloud/iot/valve-controller/internal/scheduler"
)

// Handler holds the dependencies for REST handlers.
type Handler struct {
	sched   *scheduler.Scheduler
	mqttMgr *mqttclient.Manager
}

// valveResponse is the REST representation of a managed valve.
type valveResponse struct {
	Name                  string          `json:"name"`
	Namespace             string          `json:"namespace"`
	State                 string          `json:"state"`
	Disabled              bool            `json:"disabled"`
	FriendlyName          string          `json:"friendlyName"`
	BridgeName            string          `json:"bridgeName"`
	LinkQuality           int32           `json:"linkQuality"`
	BatteryPercentage     int32           `json:"batteryPercentage"`
	Reachable             bool            `json:"reachable"`
	DailyIrrigationVolume float64         `json:"dailyIrrigationVolume"`
	LastValveOpenDuration int64           `json:"lastValveOpenDurationMs"`
	LastOpenTime          *time.Time      `json:"lastOpenTime,omitempty"`
	LastCloseTime         *time.Time      `json:"lastCloseTime,omitempty"`
	CurrentAction         *actionResponse `json:"currentAction,omitempty"`
}

// actionResponse is the REST representation of an action.
type actionResponse struct {
	ID          string  `json:"id"`
	ValveName   string  `json:"valve"`
	Type        string  `json:"type"`
	State       string  `json:"state"`
	Duration    string  `json:"duration,omitempty"`
	RequestedAt string  `json:"requestedAt"`
	FulfilledAt *string `json:"fulfilledAt,omitempty"`
	ClosedAt    *string `json:"closedAt,omitempty"`
	ExpiresAt   *string `json:"expiresAt,omitempty"`
	FailedAt    *string `json:"failedAt,omitempty"`
	Attempts    int     `json:"attempts"`
	Error       string  `json:"error,omitempty"`
}

func NewHandler(sched *scheduler.Scheduler, mqttMgr *mqttclient.Manager) *Handler {
	return &Handler{sched: sched, mqttMgr: mqttMgr}
}

// ListValves returns all managed valves.
func (h *Handler) ListValves(w http.ResponseWriter, r *http.Request) {
	keys := h.sched.ListValves()
	items := make([]valveResponse, 0, len(keys))
	for _, key := range keys {
		info, err := h.sched.GetValveInfo(key)
		if err != nil {
			continue
		}
		items = append(items, toValveResponse(info, h.sched))
	}
	writeJSON(w, http.StatusOK, collectionResponse[valveResponse]{
		Items:  items,
		Total:  len(items),
		Limit:  len(items),
		Offset: 0,
	})
}

// GetValve returns details for a single valve.
func (h *Handler) GetValve(w http.ResponseWriter, r *http.Request) {
	key, err := h.resolveValveKey(r)
	if err != nil {
		writeError(w, err)
		return
	}
	info, err := h.sched.GetValveInfo(key)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toValveResponse(info, h.sched))
}

// OpenValve enqueues an open action. Optional ?duration= query param accepts Go duration strings (e.g. "15m").
func (h *Handler) OpenValve(w http.ResponseWriter, r *http.Request) {
	key, err := h.resolveValveKey(r)
	if err != nil {
		writeError(w, err)
		return
	}

	var dur time.Duration
	if d := r.URL.Query().Get("duration"); d != "" {
		dur, err = time.ParseDuration(d)
		if err != nil {
			http.Error(w, `{"error":"invalid duration format","code":"INVALID_INPUT"}`, http.StatusUnprocessableEntity)
			return
		}
		if dur <= 0 {
			http.Error(w, `{"error":"duration must be positive","code":"INVALID_INPUT"}`, http.StatusUnprocessableEntity)
			return
		}
	}

	action, err := h.sched.Open(key, dur)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toActionResponse(action))
}

// CloseValve enqueues a close action.
func (h *Handler) CloseValve(w http.ResponseWriter, r *http.Request) {
	key, err := h.resolveValveKey(r)
	if err != nil {
		writeError(w, err)
		return
	}
	action, err := h.sched.Close(key)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toActionResponse(action))
}

// ListActions returns the action history for a valve.
func (h *Handler) ListActions(w http.ResponseWriter, r *http.Request) {
	key, err := h.resolveValveKey(r)
	if err != nil {
		writeError(w, err)
		return
	}
	actions, err := h.sched.ListActions(key)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]actionResponse, len(actions))
	for i, a := range actions {
		items[i] = toActionResponse(a)
	}
	writeJSON(w, http.StatusOK, collectionResponse[actionResponse]{
		Items:  items,
		Total:  len(items),
		Limit:  len(items),
		Offset: 0,
	})
}

// GetAction returns a specific action by ID.
func (h *Handler) GetAction(w http.ResponseWriter, r *http.Request) {
	key, err := h.resolveValveKey(r)
	if err != nil {
		writeError(w, err)
		return
	}
	actionID := chi.URLParam(r, "actionID")
	action, err := h.sched.GetAction(key, actionID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toActionResponse(action))
}

// Healthz is a liveness probe — always returns 200.
func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz checks MQTT connectivity and reports readiness.
func (h *Handler) Readyz(w http.ResponseWriter, _ *http.Request) {
	states := h.mqttMgr.ConnectionStates()
	allConnected := true
	for _, connected := range states {
		if !connected {
			allConnected = false
			break
		}
	}
	if !allConnected {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not ready", "mqtt": states})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "mqtt": states})
}

// resolveValveKey returns the scheduler key for the valve in the request.
// If namespace is provided via query param or X-Namespace header, uses "ns/name".
// Otherwise uses FindKey to resolve by name across all registered valves.
func (h *Handler) resolveValveKey(r *http.Request) (string, error) {
	name := chi.URLParam(r, "name")
	if strings.Contains(name, "/") {
		return name, nil
	}
	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		ns = r.Header.Get("X-Namespace")
	}
	if ns != "" {
		return ns + "/" + name, nil
	}
	return h.sched.FindKey(name)
}

func toValveResponse(info device.ValveInfo, sched *scheduler.Scheduler) valveResponse {
	resp := valveResponse{
		Name:                  info.Name,
		Namespace:             info.Namespace,
		State:                 string(info.State),
		Disabled:              info.Config.Disabled,
		FriendlyName:          info.FriendlyName,
		BridgeName:            info.BridgeName,
		LinkQuality:           info.LinkQuality,
		BatteryPercentage:     info.BatteryPercentage,
		Reachable:             info.Reachable,
		DailyIrrigationVolume: info.DailyIrrigationVolume,
		LastValveOpenDuration: info.LastValveOpenDuration,
	}
	if info.LastOpenTime != nil {
		resp.LastOpenTime = info.LastOpenTime
	}
	if info.LastCloseTime != nil {
		resp.LastCloseTime = info.LastCloseTime
	}
	actions, _ := sched.ListActions(info.Key())
	if len(actions) > 0 && !actions[0].IsTerminal() {
		ar := toActionResponse(actions[0])
		resp.CurrentAction = &ar
	}
	return resp
}

func toActionResponse(a *scheduler.Action) actionResponse {
	resp := actionResponse{
		ID:          a.ID,
		ValveName:   a.ValveName,
		Type:        string(a.Type),
		State:       string(a.State),
		RequestedAt: a.RequestedAt.Format(time.RFC3339Nano),
		Attempts:    a.Attempts,
		Error:       a.ErrorMessage,
	}
	if a.Duration > 0 {
		resp.Duration = a.Duration.String()
	}
	if a.FulfilledAt != nil {
		s := a.FulfilledAt.Format(time.RFC3339Nano)
		resp.FulfilledAt = &s
	}
	if a.ClosedAt != nil {
		s := a.ClosedAt.Format(time.RFC3339Nano)
		resp.ClosedAt = &s
	}
	if a.ExpiresAt != nil {
		s := a.ExpiresAt.Format(time.RFC3339Nano)
		resp.ExpiresAt = &s
	}
	if a.FailedAt != nil {
		s := a.FailedAt.Format(time.RFC3339Nano)
		resp.FailedAt = &s
	}
	return resp
}
