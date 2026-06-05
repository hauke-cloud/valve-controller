# controller-gen / kubebuilder Rules

## Makefile targets

### Always delete zz_generated.deepcopy.go before regenerating

The `generate` target **must** delete `zz_generated.deepcopy.go` before running controller-gen:

```makefile
generate:
    rm -f api/v1alpha1/zz_generated.deepcopy.go
    $(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."
```

**Why:** controller-gen v0.17 inspects the existing file. When it finds `DeepCopyInto` methods
already defined (from a previous generate or a hand-written bootstrap), it skips regenerating
them. It then overwrites the file — losing those implementations while keeping all the calls
to them. The result is `"type X has no field or method DeepCopyInto"` at every call site.
Deleting the file forces a clean generation where every required method is emitted fresh.

### generate vs manifests

`make generate` and `make manifests` must be separate invocations of controller-gen:

```makefile
generate:
    $(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

manifests:
    $(CONTROLLER_GEN) crd \
        rbac:roleName=<role> \
        paths="./api/..." \
        output:crd:artifacts:config=config/crd/bases
```

**Never** include `object:` in the `manifests` target. Running both generators in the same
controller-gen invocation (e.g. `crd rbac:... object:...`) causes the `object` generator to
overwrite `zz_generated.deepcopy.go` with an incomplete file — top-level type methods are
emitted but nested Spec/Status `DeepCopyInto` methods are silently dropped, producing
`"type X has no field or method DeepCopyInto"` compile errors.

### crd generator options

`crd:trivialVersions=false` was removed in controller-gen v0.8. Do **not** use it.
The correct invocation is just `crd` (multi-version handling is always on):

```makefile
# WRONG (will fail with "unknown argument trivialVersions"):
$(CONTROLLER_GEN) crd:trivialVersions=false ...

# CORRECT:
$(CONTROLLER_GEN) crd ...
```

### setup-envtest binary directory

`setup-envtest` must write to a directory the CI runner owns. `/usr/local/kubebuilder`
requires root and will fail with "permission denied". Use a project-local path:

```makefile
# WRONG:
$(ENVTEST) use --bin-dir /usr/local/kubebuilder/bin

# CORRECT:
$(ENVTEST) use --bin-dir ./bin/envtest
```

## CI pre-build order

Always run in this order — each step is a prerequisite for the next:

```yaml
pre-build-command: |
  go mod tidy        # creates go.sum; required before controller-gen can load packages
  go mod download    # warm the module cache
  make generate      # writes zz_generated.deepcopy.go
  make manifests     # writes CRD YAML + RBAC (no deepcopy)
  make setup-envtest # downloads envtest binaries
  gofmt -s -w .     # auto-format so gofmt -s -l . finds nothing during lint
```

`go mod tidy` **must** run before `make generate`. controller-gen uses `go/packages` to
type-check the project; if `go.sum` is missing it cannot resolve imports and reports
"missing go.sum entry" and cascading "invalid field type: invalid type" errors for every
embedded `metav1.TypeMeta` / `metav1.ObjectMeta` field.
