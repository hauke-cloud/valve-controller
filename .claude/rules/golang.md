# Go Project Rules

## Project Layout

Follow standard Go project layout:

```
cmd/controller/main.go          # binary entry point — flags, signal handling, wiring only
internal/mqtt/                  # MQTT client, Tasmota topic parsing, message dispatch
internal/api/                   # REST API handlers, middleware, router setup
internal/k8s/                   # Kubernetes controller, reconciler logic
internal/db/                    # TimescaleDB client, query functions, migrations runner
internal/device/                # Device domain types, state machine, capabilities
api/v1alpha1/                   # CRD Go types (DeepCopyObject, GroupVersionKind)
config/crd/bases/               # Generated CRD YAML manifests
migrations/                     # Ordered SQL migration files (001_init.sql, 002_…)
```

## Code Conventions

- Use `context.Context` as the first argument to every function that does I/O.
- Wrap errors with `fmt.Errorf("…: %w", err)` — never discard them silently.
- Define sentinel errors as package-level `var ErrX = errors.New("…")` so callers can use `errors.Is`.
- Prefer table-driven tests (`[]struct{ name, input, want }`). Name sub-tests after `tt.name`.
- Keep `main.go` thin: parse flags → build deps → start components → block on `ctx.Done()`.
- Use `slog` (stdlib) for structured logging. Pass a `*slog.Logger` through constructors, never use a global logger.
- Return concrete types from constructors (`*Client`, not an interface). Expose interfaces at call sites where you need to mock.
- Never `panic` in library code — only in `main` during startup validation.

## Error Handling

- HTTP handlers: write a single `writeError(w, status, err)` helper — never inline `http.Error` calls scattered across handlers.
- MQTT callbacks: log the error and return — never crash the subscriber goroutine.
- Database: wrap `pgx` errors and check `pgconn.PgError.Code` for constraint violations before returning a generic error.

## Dependencies

- `github.com/eclipse/paho.mqtt.golang` — MQTT client
- `github.com/jackc/pgx/v5` — PostgreSQL/TimescaleDB driver (use `pgxpool`)
- `sigs.k8s.io/controller-runtime` — Kubernetes controller
- `k8s.io/apimachinery`, `k8s.io/client-go` — core Kubernetes types
- `github.com/go-chi/chi/v5` — HTTP router
- `github.com/go-playground/validator/v10` — request body validation
- Prefer stdlib over third-party for anything the stdlib can do well.

## Formatting & Linting

- `gofmt` / `goimports` on every file.
- `golangci-lint` with at minimum: `errcheck`, `govet`, `staticcheck`, `revive`.
- Maximum line length: 120 characters (mirrors `.editorconfig`).
