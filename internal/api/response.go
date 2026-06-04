package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hauke-cloud/iot/valve-controller/internal/scheduler"
)

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

type collectionResponse[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	code, status := classifyError(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: err.Error(), Code: code})
}

func classifyError(err error) (code string, status int) {
	switch {
	case errors.Is(err, scheduler.ErrValveNotFound):
		return "VALVE_NOT_FOUND", http.StatusNotFound
	case errors.Is(err, scheduler.ErrValveDisabled):
		return "VALVE_DISABLED", http.StatusUnprocessableEntity
	default:
		return "INTERNAL_ERROR", http.StatusInternalServerError
	}
}
