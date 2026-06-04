package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	iotv1alpha1 "github.com/hauke-cloud/iot/valve-controller/api/v1alpha1"
	restapi "github.com/hauke-cloud/iot/valve-controller/internal/api"
	"github.com/hauke-cloud/iot/valve-controller/internal/k8s"
	"github.com/hauke-cloud/iot/valve-controller/internal/metrics"
	"github.com/hauke-cloud/iot/valve-controller/internal/mqtt"
	"github.com/hauke-cloud/iot/valve-controller/internal/scheduler"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(iotv1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		apiAddr     = flag.String("api-addr", ":8443", "Address for the mTLS REST API server")
		metricsAddr = flag.String("metrics-addr", ":8080", "Address for controller-runtime metrics")
		probeAddr   = flag.String("probe-addr", ":8081", "Address for liveness/readiness probes")
		tlsCert     = flag.String("tls-cert", "/tls/tls.crt", "Path to server TLS certificate")
		tlsKey      = flag.String("tls-key", "/tls/tls.key", "Path to server TLS private key")
		tlsClientCA = flag.String("tls-client-ca", "/tls/ca.crt", "Path to CA cert for client cert verification")
		logLevel    = flag.String("log-level", "info", "Log level: debug|info|warn|error")
	)
	flag.Parse()

	lvl := slog.LevelInfo
	switch *logLevel {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	// controller-runtime exposes its own registry; cast it to both interfaces we need.
	promReg := ctrlmetrics.Registry.(prometheus.Registerer)
	promGatherer := ctrlmetrics.Registry.(prometheus.Gatherer)
	m := metrics.New(promReg)

	mqttMgr := mqtt.NewManager(hostname, m, log.With("component", "mqtt"))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: *metricsAddr,
		},
		HealthProbeBindAddress: *probeAddr,
	})
	if err != nil {
		log.Error("failed to create controller manager", "err", err)
		os.Exit(1)
	}

	valveReconciler := &k8s.MQTTValveReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    log.With("reconciler", "mqttvalve"),
	}

	sched := scheduler.New(mqttMgr, valveReconciler, m, log.With("component", "scheduler"))
	valveReconciler.Scheduler = sched

	if err := valveReconciler.SetupWithManager(mgr); err != nil {
		log.Error("failed to register MQTTValve controller", "err", err)
		os.Exit(1)
	}

	bridgeReconciler := &k8s.MQTTBridgeReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Log:     log.With("reconciler", "mqttbridge"),
		Manager: mqttMgr,
	}
	if err := bridgeReconciler.SetupWithManager(mgr); err != nil {
		log.Error("failed to register MQTTBridge controller", "err", err)
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error("failed to add healthz check", "err", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error("failed to add readyz check", "err", err)
		os.Exit(1)
	}

	tlsCfg, err := restapi.TLSConfig(*tlsCert, *tlsKey, *tlsClientCA)
	if err != nil {
		log.Error("failed to build TLS config", "err", err)
		os.Exit(1)
	}

	apiServer := &http.Server{
		Addr:      *apiAddr,
		Handler:   restapi.NewRouter(restapi.NewHandler(sched, mqttMgr), promGatherer),
		TLSConfig: tlsCfg,
	}

	go func() {
		log.Info("REST API listening", "addr", *apiAddr, "mode", "mTLS")
		if err := apiServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Error("REST API server error", "err", err)
		}
	}()

	log.Info("starting controller manager")
	if err := mgr.Start(ctx); err != nil {
		log.Error("manager stopped with error", "err", err)
		os.Exit(1)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := apiServer.Shutdown(shutdownCtx); err != nil {
		log.Warn("REST API shutdown error", "err", err)
	}
	mqttMgr.StopAll()
	log.Info("shutdown complete")
}
