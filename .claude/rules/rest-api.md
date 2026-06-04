# REST API Rules

## Router & Middleware

Use `github.com/go-chi/chi/v5`. Mount all routes under `/api/v1/`.

Required middleware stack (applied in order):

```go
r.Use(middleware.RequestID)
r.Use(middleware.RealIP)
r.Use(middleware.Logger)         // or a slog-backed equivalent
r.Use(middleware.Recoverer)
r.Use(middleware.Timeout(30 * time.Second))
```

## Route Naming

```
GET    /api/v1/devices                   → list all Zigbee devices (mirrors CRs)
GET    /api/v1/devices/{name}            → get one device (name = CR metadata.name)
POST   /api/v1/devices/{name}/command    → send a command to a device
GET    /api/v1/devices/{name}/readings   → list recent sensor readings (paginated)
GET    /api/v1/healthz                   → liveness probe (no auth)
GET    /api/v1/readyz                    → readiness probe (checks MQTT + DB, no auth)
```

## Request / Response Format

- Content-Type: `application/json` on all endpoints.
- Always decode the request body into a typed struct; validate with `go-playground/validator/v10`.
- Unified error envelope:

```json
{ "error": "device not found", "code": "DEVICE_NOT_FOUND" }
```

- Success envelope for collections:

```json
{ "items": [...], "total": 42, "limit": 20, "offset": 0 }
```

- Timestamps: RFC 3339 (`time.RFC3339Nano`), always UTC.

## Error → HTTP Status Mapping

| Sentinel / condition | HTTP status |
|----------------------|-------------|
| `ErrNotFound` | 404 |
| `ErrAlreadyExists` | 409 |
| `ErrInvalidInput` / validation failure | 422 |
| `ErrDeviceUnreachable` | 503 |
| Any other internal error | 500 |

Implement a single `writeError(w, err)` helper in `internal/api/response.go` that does this mapping — never call `http.Error` directly in handlers.

## Handler Structure

Handlers are thin: decode → validate → call service → encode. No business logic in handlers.

```go
func (h *Handler) GetDevice(w http.ResponseWriter, r *http.Request) {
    name := chi.URLParam(r, "name")
    dev, err := h.devices.Get(r.Context(), name)
    if err != nil {
        writeError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, dev)
}
```

## Authentication

All routes except `/healthz` and `/readyz` require a Bearer token validated against the Kubernetes `TokenReview` API (reuse the in-cluster service account). Apply as a middleware.

## Pagination

Default `limit=20`, max `limit=100`. Accept `limit` and `offset` query parameters. Parse and validate them in a shared `parsePagination(r *http.Request) (limit, offset int, err error)` helper.

## OpenAPI

Maintain `api/openapi.yaml`. Update it whenever a route, field, or error code changes. Validate it in CI with `go tool openapi-lint` or equivalent.
