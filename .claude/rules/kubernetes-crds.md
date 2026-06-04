# Kubernetes CRD Rules

## Group / Version / Kind

- API group: `iot.hauke.cloud`
- Initial version: `v1alpha1` (promote to `v1beta1` / `v1` once the schema stabilizes)
- CRD YAML lives in `config/crd/bases/`, generated via `controller-gen`.

## Type Definitions (`api/v1alpha1/`)

One file per Kind. Required boilerplate for every type:

```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=zd
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type ZigbeeDevice struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   ZigbeeDeviceSpec   `json:"spec"`
    Status ZigbeeDeviceStatus `json:"status,omitempty"`
}
```

- `Spec` fields: use `+kubebuilder:validation:*` markers for all required/enum/min-max constraints.
- `Status` must contain a `Conditions []metav1.Condition` field — update via `meta.SetStatusCondition`.
- Always implement `DeepCopyObject()` — run `controller-gen object` to generate.

## Spec Design

```go
type ZigbeeDeviceSpec struct {
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    DeviceName string `json:"deviceName"`          // matches Tasmota friendly name

    // +kubebuilder:validation:Required
    BridgeTopic string `json:"bridgeTopic"`         // MQTT bridge topic prefix

    // +kubebuilder:validation:Enum=valve;sensor;switch
    DeviceType DeviceType `json:"deviceType"`

    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=65535
    PollIntervalSeconds int32 `json:"pollIntervalSeconds,omitempty"`
}
```

## Controller / Reconciler (`internal/k8s/`)

- One reconciler per Kind. Implement `reconcile.Reconciler`.
- Always use `ctrl.Result{}` with `RequeueAfter` rather than returning errors for transient failures you can retry.
- Fetch the object first; return `nil` on `apierrors.IsNotFound` (object was deleted).
- Use server-side apply (`Patch` with `ssa` field manager) when creating/updating owned resources.
- Record events via `record.EventRecorder` — `Warning` for errors, `Normal` for state transitions.
- Set `controller-runtime`'s `MaxConcurrentReconciles` to 5; each reconcile must be idempotent.

```go
func (r *ZigbeeDeviceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := r.Log.With("device", req.NamespacedName)

    var device iov1.ZigbeeDevice
    if err := r.Get(ctx, req.NamespacedName, &device); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // … reconcile logic …

    return ctrl.Result{RequeueAfter: time.Duration(device.Spec.PollIntervalSeconds) * time.Second}, nil
}
```

## Ownership and Garbage Collection

- Set `OwnerReference` on any resource (ConfigMap, Secret, child CRs) the controller creates.
- Use `ctrl.SetControllerReference(owner, owned, r.Scheme)`.

## Generating Manifests

```bash
# Re-run after every change to api/v1alpha1/
controller-gen crd:trivialVersions=false rbac:roleName=controller-role \
  object:headerFile=hack/boilerplate.go.txt \
  paths=./api/... \
  output:crd:artifacts:config=config/crd/bases
```

Always commit the generated CRD YAML alongside the Go type changes.
