package dryrun

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
	schedulercache "github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/cache"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/carbon"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/eval"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/price"
)

// System coordinates the webhook and completion controller
type System struct {
	kubeClient kubernetes.Interface
	config     *Config
	webhook    *Webhook
	controller *CompletionController
	podStore   *PodEvaluationStore
	evaluator  *eval.Evaluator
}

// NewSystem creates a new dry-run system
func NewSystem(kubeClient kubernetes.Interface, cfg *Config) (*System, error) {
	// Initialize shared pod store
	podStore := NewPodEvaluationStore()

	// Create cache for API calls
	dataCache := schedulercache.New(5*60*1000, 30*60*1000) // 5 min TTL, 30 min max age

	// Initialize carbon implementation if enabled
	var carbonImpl carbon.Implementation
	if cfg.Carbon.Enabled {
		if cfg.Carbon.APIKey == "" {
			return nil, fmt.Errorf("carbon evaluation enabled but no API key provided")
		}

		apiConfig := config.ElectricityMapsAPIConfig{
			APIKey: cfg.Carbon.APIKey,
			Region: cfg.Carbon.Region,
			URL:    "https://api.electricitymap.org/v3",
		}

		cacheConfig := config.APICacheConfig{
			CacheTTL: 5 * 60 * 1000, // 5 minutes
		}

		apiClient := api.NewClient(apiConfig, cacheConfig, api.WithCache(dataCache))

		carbonConfig := &config.CarbonConfig{
			Enabled:            true,
			Provider:           "electricity-maps-api",
			IntensityThreshold: cfg.Carbon.Threshold,
			APIConfig:          apiConfig,
		}

		carbonImpl = carbon.New(carbonConfig, apiClient)
		klog.InfoS("Carbon evaluation initialized",
			"region", cfg.Carbon.Region,
			"threshold", cfg.Carbon.Threshold)
	} else {
		klog.InfoS("Carbon evaluation disabled")
	}

	// Initialize pricing implementation if enabled
	var priceImpl price.Implementation
	if cfg.Pricing.Enabled {
		// TODO: Load TOU schedules from ConfigMap
		// For now, pricing is disabled in dry-run mode
		klog.InfoS("Price evaluation not yet implemented in dry-run mode")
		priceImpl = nil
	}

	// Create shared evaluator
	evalConfig := &config.Config{
		Carbon: config.CarbonConfig{
			Enabled:            cfg.Carbon.Enabled,
			IntensityThreshold: cfg.Carbon.Threshold,
		},
		Pricing: config.PriceConfig{
			Enabled: cfg.Pricing.Enabled,
		},
	}
	evaluator := eval.NewEvaluator(carbonImpl, priceImpl, evalConfig)

	// Create webhook
	webhook := NewWebhook(cfg, evaluator, podStore)

	// Create completion controller
	controller := NewCompletionController(kubeClient, cfg, podStore)

	system := &System{
		kubeClient: kubeClient,
		config:     cfg,
		webhook:    webhook,
		controller: controller,
		podStore:   podStore,
		evaluator:  evaluator,
	}

	return system, nil
}

// WebhookHandler returns the HTTP handler for the webhook
func (s *System) WebhookHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", s.webhook.ServeHTTP)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	return mux
}

// MetricsHandler returns the HTTP handler for metrics
func (s *System) MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// RunController starts the completion controller
func (s *System) RunController(ctx context.Context) error {
	return s.controller.Run(ctx)
}
