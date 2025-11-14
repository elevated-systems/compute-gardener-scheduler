package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/dryrun"
)

func main() {
	var (
		webhookPort     int
		metricsPort     int
		mode            string
		watchNamespaces stringSlice
		carbonEnabled   bool
		carbonRegion    string
		carbonThreshold float64
		carbonAPIKey    string
		pricingEnabled  bool
		tlsCertPath     string
		tlsKeyPath      string
	)

	flag.IntVar(&webhookPort, "webhook-port", 8443, "Webhook server port")
	flag.IntVar(&metricsPort, "metrics-port", 8080, "Metrics server port")
	flag.StringVar(&mode, "mode", "metrics", "Dry-run mode: 'metrics' or 'annotate'")
	flag.Var(&watchNamespaces, "watch-namespace", "Namespace to watch (can be specified multiple times)")
	flag.BoolVar(&carbonEnabled, "carbon-enabled", true, "Enable carbon-aware evaluation")
	flag.StringVar(&carbonRegion, "carbon-region", "US-CAL-CISO", "Electricity Maps region")
	flag.Float64Var(&carbonThreshold, "carbon-threshold", 200.0, "Default carbon intensity threshold")
	flag.StringVar(&carbonAPIKey, "carbon-api-key", "", "Electricity Maps API key (or set ELECTRICITY_MAPS_API_KEY)")
	flag.BoolVar(&pricingEnabled, "pricing-enabled", false, "Enable price-aware evaluation")
	flag.StringVar(&tlsCertPath, "tls-cert", "/etc/webhook/certs/tls.crt", "Path to TLS certificate")
	flag.StringVar(&tlsKeyPath, "tls-key", "/etc/webhook/certs/tls.key", "Path to TLS key")

	klog.InitFlags(nil)
	flag.Parse()

	// Validate mode
	if mode != "metrics" && mode != "annotate" {
		klog.ErrorS(nil, "Invalid mode, must be 'metrics' or 'annotate'", "mode", mode)
		os.Exit(1)
	}

	// Get API key from environment if not provided via flag
	if carbonAPIKey == "" {
		carbonAPIKey = os.Getenv("ELECTRICITY_MAPS_API_KEY")
	}

	klog.InfoS("Starting Compute Gardener Dry-Run System",
		"webhookPort", webhookPort,
		"metricsPort", metricsPort,
		"mode", mode,
		"watchNamespaces", watchNamespaces,
		"carbonEnabled", carbonEnabled,
		"pricingEnabled", pricingEnabled)

	// Create in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.ErrorS(err, "Failed to create in-cluster config")
		os.Exit(1)
	}

	// Create Kubernetes client
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.ErrorS(err, "Failed to create Kubernetes client")
		os.Exit(1)
	}

	// Create dry-run configuration
	dryRunConfig := &dryrun.Config{
		Mode:            mode,
		WatchNamespaces: watchNamespaces,
		Carbon: dryrun.CarbonConfig{
			Enabled:   carbonEnabled,
			Region:    carbonRegion,
			Threshold: carbonThreshold,
			APIKey:    carbonAPIKey,
		},
		Pricing: dryrun.PricingConfig{
			Enabled: pricingEnabled,
		},
	}

	// Create dry-run system (webhook + controller)
	system, err := dryrun.NewSystem(kubeClient, dryRunConfig)
	if err != nil {
		klog.ErrorS(err, "Failed to create dry-run system")
		os.Exit(1)
	}

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		klog.InfoS("Received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Start completion controller in background
	go func() {
		if err := system.RunController(ctx); err != nil {
			klog.ErrorS(err, "Completion controller error")
			cancel()
		}
	}()

	// Start metrics server if in metrics mode
	if mode == "metrics" {
		go func() {
			metricsServer := &http.Server{
				Addr:    fmt.Sprintf(":%d", metricsPort),
				Handler: system.MetricsHandler(),
			}
			klog.InfoS("Starting metrics server", "port", metricsPort)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				klog.ErrorS(err, "Metrics server error")
			}
		}()
	}

	// Start webhook server
	webhookServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", webhookPort),
		Handler:      system.WebhookHandler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	klog.InfoS("Starting webhook server", "port", webhookPort)
	go func() {
		if err := webhookServer.ListenAndServeTLS(tlsCertPath, tlsKeyPath); err != nil && err != http.ErrServerClosed {
			klog.ErrorS(err, "Webhook server error")
			cancel()
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	klog.InfoS("Shutting down servers")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := webhookServer.Shutdown(shutdownCtx); err != nil {
		klog.ErrorS(err, "Error shutting down webhook server")
	}

	klog.InfoS("Dry-run system stopped")
}

// stringSlice implements flag.Value for repeated string flags
type stringSlice []string

func (s *stringSlice) String() string {
	return fmt.Sprintf("%v", *s)
}

func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}
