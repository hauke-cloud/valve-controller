package api

import (
	"fmt"
	"net/http"
	"strconv"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

func parsePagination(r *http.Request) (limit, offset int, err error) {
	limit = defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil || limit < 1 {
			return 0, 0, fmt.Errorf("invalid limit: must be a positive integer")
		}
		if limit > maxLimit {
			return 0, 0, fmt.Errorf("invalid limit: maximum is %d", maxLimit)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, err = strconv.Atoi(v)
		if err != nil || offset < 0 {
			return 0, 0, fmt.Errorf("invalid offset: must be a non-negative integer")
		}
	}
	return limit, offset, nil
}
