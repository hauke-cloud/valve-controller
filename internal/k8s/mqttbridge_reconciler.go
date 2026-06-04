package k8s

import (
	"context"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	iotv1alpha1 "github.com/hauke-cloud/iot/valve-controller/api/v1alpha1"
	mqttclient "github.com/hauke-cloud/iot/valve-controller/internal/mqtt"
)

// MQTTBridgeReconciler manages MQTT client connections for each MQTTBridge CR.
//
// +kubebuilder:rbac:groups=iot.hauke.cloud,resources=mqttbridges,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
type MQTTBridgeReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Log     *slog.Logger
	Manager *mqttclient.Manager
}

func (r *MQTTBridgeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.With("bridge", req.NamespacedName)

	var bridge iotv1alpha1.MQTTBridge
	if err := r.Get(ctx, req.NamespacedName, &bridge); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !bridge.DeletionTimestamp.IsZero() {
		r.Manager.Remove(bridge.Spec.BridgeName)
		return ctrl.Result{}, nil
	}

	port := bridge.Spec.Port
	if port == 0 {
		port = 1883
	}

	cfg := mqttclient.BridgeConfig{
		BridgeName:          bridge.Spec.BridgeName,
		Host:                bridge.Spec.Host,
		Port:                port,
		MaxReconnectBackoff: bridge.Spec.MaxReconnectBackoffSeconds,
	}

	if ref := bridge.Spec.CredentialsSecretRef; ref != nil {
		username, password, err := r.readCredentials(ctx, ref)
		if err != nil {
			log.Warn("failed to read bridge credentials", "err", err)
			return ctrl.Result{}, fmt.Errorf("read credentials: %w", err)
		}
		cfg.Username = username
		cfg.Password = password
	}

	if err := r.Manager.Upsert(ctx, cfg); err != nil {
		log.Warn("failed to connect to MQTT broker", "err", err)
		return ctrl.Result{}, fmt.Errorf("upsert bridge: %w", err)
	}

	log.Info("bridge reconciled", "host", bridge.Spec.Host, "port", port)
	return ctrl.Result{}, nil
}

func (r *MQTTBridgeReconciler) readCredentials(ctx context.Context, ref *iotv1alpha1.BridgeCredentialsRef) (string, string, error) {
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}, &secret); err != nil {
		return "", "", fmt.Errorf("get secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	usernameKey := ref.UsernameKey
	if usernameKey == "" {
		usernameKey = "username"
	}
	passwordKey := ref.PasswordKey
	if passwordKey == "" {
		passwordKey = "password"
	}
	return string(secret.Data[usernameKey]), string(secret.Data[passwordKey]), nil
}

func (r *MQTTBridgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&iotv1alpha1.MQTTBridge{}).
		Complete(r)
}
