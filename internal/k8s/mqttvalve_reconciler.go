package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	iotv1alpha1 "github.com/hauke-cloud/iot/valve-controller/api/v1alpha1"
	"github.com/hauke-cloud/iot/valve-controller/internal/device"
	"github.com/hauke-cloud/iot/valve-controller/internal/scheduler"
)

// MQTTValveReconciler reconciles MQTTValve resources.
//
// +kubebuilder:rbac:groups=iot.hauke.cloud,resources=mqttvalves,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=iot.hauke.cloud,resources=mqttvalves/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=iot.hauke.cloud,resources=mqttdevices,verbs=get;list;watch
// +kubebuilder:rbac:groups=iot.hauke.cloud,resources=mqttbridges,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
type MQTTValveReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Log       *slog.Logger
	Scheduler *scheduler.Scheduler
}

func (r *MQTTValveReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.With("valve", req.NamespacedName)

	var valve iotv1alpha1.MQTTValve
	if err := r.Get(ctx, req.NamespacedName, &valve); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !valve.DeletionTimestamp.IsZero() {
		r.Scheduler.DeregisterValve(req.NamespacedName.String())
		return ctrl.Result{}, nil
	}

	// Find the MQTTDevice that references this valve via typeRef.
	dev, err := r.findDevice(ctx, valve.Name, valve.Namespace)
	if err != nil {
		log.Warn("could not find linked MQTTDevice", "err", err)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if dev == nil {
		log.Info("no MQTTDevice linked yet, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Resolve the MQTTBridge.
	var bridge iotv1alpha1.MQTTBridge
	bridgeKey := types.NamespacedName{Name: dev.Spec.BridgeRef.Name, Namespace: dev.Spec.BridgeRef.Namespace}
	if err := r.Get(ctx, bridgeKey, &bridge); err != nil {
		log.Warn("could not get MQTTBridge", "bridge", bridgeKey, "err", err)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	port := bridge.Spec.Port
	if port == 0 {
		port = 1883
	}
	keepClosed := time.Duration(valve.Spec.KeepClosedIntervalSeconds) * time.Second
	if keepClosed == 0 {
		keepClosed = 5 * time.Minute
	}
	cmdTimeout := time.Duration(valve.Spec.CommandTimeoutSeconds) * time.Second
	if cmdTimeout == 0 {
		cmdTimeout = 30 * time.Second
	}
	retryCount := int(valve.Spec.RetryCount)
	if retryCount == 0 {
		retryCount = 3
	}
	closeRepeat := int(valve.Spec.CloseRepeatCount)
	if closeRepeat == 0 {
		closeRepeat = 3
	}
	maxOpen := time.Duration(valve.Spec.MaxOpenDurationSeconds) * time.Second

	info := device.ValveInfo{
		Name:         valve.Name,
		Namespace:    valve.Namespace,
		FriendlyName: dev.Spec.FriendlyName,
		BridgeName:   bridge.Spec.BridgeName,
		BridgeHost:   bridge.Spec.Host,
		BridgePort:   port,
		State:        device.ValveState(valve.Status.ValveState),
		Config: device.ValveConfig{
			Disabled:           valve.Spec.Disabled || dev.Spec.Disabled,
			RetryCount:         retryCount,
			CommandTimeout:     cmdTimeout,
			KeepClosedInterval: keepClosed,
			MaxOpenDuration:    maxOpen,
			CloseRepeatCount:   closeRepeat,
		},
	}

	r.Scheduler.RegisterValve(ctx, info)

	if err := r.patchReadyCondition(ctx, &valve, true, "Configured", "valve pipeline running"); err != nil {
		log.Warn("failed to patch Ready condition", "err", err)
	}

	log.Debug("valve reconciled", "device", dev.Spec.FriendlyName, "bridge", bridge.Spec.BridgeName)
	return ctrl.Result{}, nil
}

// UpdateValveStatus is the StatusWriter implementation called by pipeline goroutines.
func (r *MQTTValveReconciler) UpdateValveStatus(
	ctx context.Context, name, namespace string, state device.ValveState, actionID string,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var valve iotv1alpha1.MQTTValve
		if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &valve); err != nil {
			return client.IgnoreNotFound(err)
		}

		valve.Status.ValveState = iotv1alpha1.ValveState(state)
		valve.Status.CurrentActionID = actionID
		now := metav1.Now()
		switch state {
		case device.ValveStateOpen:
			valve.Status.LastOpenTime = &now
		case device.ValveStateClosed:
			valve.Status.LastCloseTime = &now
		}

		return r.Status().Update(ctx, &valve)
	})
}

func (r *MQTTValveReconciler) findDevice(ctx context.Context, valveName, namespace string) (*iotv1alpha1.MQTTDevice, error) {
	var list iotv1alpha1.MQTTDeviceList
	if err := r.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("list MQTTDevices: %w", err)
	}
	for i := range list.Items {
		dev := &list.Items[i]
		if dev.Spec.TypeRef == nil {
			continue
		}
		if dev.Spec.TypeRef.Kind == "MQTTValve" &&
			dev.Spec.TypeRef.Name == valveName &&
			dev.Spec.TypeRef.Namespace == namespace {
			return dev, nil
		}
	}
	return nil, nil
}

func (r *MQTTValveReconciler) patchReadyCondition(ctx context.Context, valve *iotv1alpha1.MQTTValve, ready bool, reason, msg string) error {
	condStatus := metav1.ConditionTrue
	if !ready {
		condStatus = metav1.ConditionFalse
	}
	key := types.NamespacedName{Name: valve.Name, Namespace: valve.Namespace}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, key, valve); err != nil {
			return client.IgnoreNotFound(err)
		}
		setCondition(&valve.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             condStatus,
			Reason:             reason,
			Message:            msg,
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: valve.Generation,
		})
		return r.Status().Update(ctx, valve)
	})
}

func setCondition(conditions *[]metav1.Condition, newCond metav1.Condition) {
	for i, c := range *conditions {
		if c.Type == newCond.Type {
			if c.Status == newCond.Status {
				return // no change
			}
			(*conditions)[i] = newCond
			return
		}
	}
	*conditions = append(*conditions, newCond)
}

// mapDeviceToValve maps a changed MQTTDevice to reconcile requests for its referenced MQTTValve.
func (r *MQTTValveReconciler) mapDeviceToValve(_ context.Context, obj client.Object) []reconcile.Request {
	dev, ok := obj.(*iotv1alpha1.MQTTDevice)
	if !ok || dev.Spec.TypeRef == nil || dev.Spec.TypeRef.Kind != "MQTTValve" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name:      dev.Spec.TypeRef.Name,
			Namespace: dev.Spec.TypeRef.Namespace,
		},
	}}
}

// mapBridgeToValves triggers reconciliation of all valves that use devices on a changed bridge.
func (r *MQTTValveReconciler) mapBridgeToValves(ctx context.Context, obj client.Object) []reconcile.Request {
	bridge, ok := obj.(*iotv1alpha1.MQTTBridge)
	if !ok {
		return nil
	}

	var devList iotv1alpha1.MQTTDeviceList
	if err := r.List(ctx, &devList); err != nil {
		return nil
	}

	var reqs []reconcile.Request
	for _, dev := range devList.Items {
		if dev.Spec.TypeRef == nil || dev.Spec.TypeRef.Kind != "MQTTValve" {
			continue
		}
		if dev.Spec.BridgeRef.Name == bridge.Name && dev.Spec.BridgeRef.Namespace == bridge.Namespace {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      dev.Spec.TypeRef.Name,
					Namespace: dev.Spec.TypeRef.Namespace,
				},
			})
		}
	}
	return reqs
}

func (r *MQTTValveReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&iotv1alpha1.MQTTValve{}).
		Watches(
			&iotv1alpha1.MQTTDevice{},
			handler.EnqueueRequestsFromMapFunc(r.mapDeviceToValve),
		).
		Watches(
			&iotv1alpha1.MQTTBridge{},
			handler.EnqueueRequestsFromMapFunc(r.mapBridgeToValves),
		).
		Complete(r)
}
