// Command fleet-record-controller runs the Kubernetes to Fleet record bridge.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	api "github.com/stolostron/ocm-fleet/controllers/record/api/v1alpha1"
	"github.com/stolostron/ocm-fleet/controllers/record/controller"
	"github.com/stolostron/ocm-fleet/controllers/record/fleet"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var endpoint, namespace, leaderNamespace, clusterID, tokenFile, caFile, metricsAddress string
	var allowHTTP, leaderElection bool
	var pollInterval time.Duration
	var concurrency int
	flag.StringVar(&endpoint, "fleet-url", "", "Fleet HTTP(S) endpoint")
	flag.StringVar(&namespace, "namespace", "", "Kubernetes namespace to watch")
	flag.StringVar(&leaderNamespace, "leader-election-namespace", os.Getenv("POD_NAMESPACE"), "Namespace for the leader lease, defaulting to POD_NAMESPACE")
	flag.StringVar(&clusterID, "source-cluster-id", "", "Stable unique identity of this Kubernetes cluster")
	flag.StringVar(&tokenFile, "token-file", "/var/run/secrets/fleet/token", "Rotating Fleet bearer token file")
	flag.StringVar(&caFile, "ca-file", "", "Optional PEM bundle added to system Fleet TLS roots")
	flag.StringVar(&metricsAddress, "metrics-bind-address", "0", "Metrics address; disabled by default")
	flag.BoolVar(&allowHTTP, "allow-http", false, "Allow plaintext Fleet HTTP for local development")
	flag.BoolVar(&leaderElection, "leader-elect", true, "Coordinate controller replicas with a Kubernetes Lease")
	flag.DurationVar(&pollInterval, "poll-interval", 30*time.Second, "Fleet drift and status refresh interval")
	flag.IntVar(&concurrency, "concurrency", 4, "Maximum concurrent reconciliations")
	logOptions := zap.Options{}
	logOptions.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logOptions)))
	if namespace == "" {
		return fmt.Errorf("--namespace is required")
	}
	if leaderElection && leaderNamespace == "" {
		return fmt.Errorf("--leader-election-namespace or POD_NAMESPACE is required when leader election is enabled")
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "https" && (!allowHTTP || u.Scheme != "http")) {
		return fmt.Errorf("--fleet-url requires HTTPS; local development may explicitly use --allow-http")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return fmt.Errorf("read Fleet CA: %w", err)
		}
		if !roots.AppendCertsFromPEM(pem) {
			return fmt.Errorf("fleet CA file has no certificates")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	defer transport.CloseIdleConnections()
	remote, err := fleet.NewClient(endpoint, &http.Client{Transport: transport, Timeout: 10 * time.Second}, func(context.Context) (string, error) {
		data, err := os.ReadFile(tokenFile)
		return string(data), err
	})
	if err != nil {
		return err
	}
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		return err
	}
	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		Cache:                   cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}},
		Metrics:                 metricsserver.Options{BindAddress: metricsAddress},
		HealthProbeBindAddress:  ":8081",
		LeaderElection:          leaderElection,
		LeaderElectionID:        "fleet-record-controller.records.open-cluster-management.io",
		LeaderElectionNamespace: leaderNamespace,
	})
	if err != nil {
		return err
	}
	reconciler, err := controller.NewReconciler(manager.GetClient(), manager.GetAPIReader(), remote, clusterID, controller.WithPollInterval(pollInterval))
	if err != nil {
		return err
	}
	if err := reconciler.SetupWithManager(manager, concurrency); err != nil {
		return err
	}
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}
	return manager.Start(ctrl.SetupSignalHandler())
}
