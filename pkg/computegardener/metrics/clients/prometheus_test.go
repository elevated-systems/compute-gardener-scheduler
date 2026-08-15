package clients

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// mockPromAPI is an in-test implementation of the prometheus v1.API interface.
// Only Query and QueryRange are exercised by PrometheusMetricsClient; the rest
// return zero values so the mock satisfies the interface.
type mockPromAPI struct {
	queryFn    func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error)
	queryRange func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error)
	lastQuery  string
	lastRangeQ string
	lastRange  v1.Range
}

func (m *mockPromAPI) Query(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
	m.lastQuery = q
	if m.queryFn != nil {
		return m.queryFn(ctx, q, ts, opts...)
	}
	return model.Vector{}, nil, nil
}

func (m *mockPromAPI) QueryRange(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
	m.lastRangeQ = q
	m.lastRange = r
	if m.queryRange != nil {
		return m.queryRange(ctx, q, r, opts...)
	}
	return model.Matrix{}, nil, nil
}

// The following satisfy the v1.API interface but are never called by the code under test.
func (m *mockPromAPI) Alerts(ctx context.Context) (v1.AlertsResult, error) {
	return v1.AlertsResult{}, nil
}
func (m *mockPromAPI) AlertManagers(ctx context.Context) (v1.AlertManagersResult, error) {
	return v1.AlertManagersResult{}, nil
}
func (m *mockPromAPI) CleanTombstones(ctx context.Context) error { return nil }
func (m *mockPromAPI) Config(ctx context.Context) (v1.ConfigResult, error) {
	return v1.ConfigResult{}, nil
}
func (m *mockPromAPI) DeleteSeries(ctx context.Context, matches []string, startTime, endTime time.Time) error {
	return nil
}
func (m *mockPromAPI) Flags(ctx context.Context) (v1.FlagsResult, error) { return nil, nil }
func (m *mockPromAPI) LabelNames(ctx context.Context, matches []string, startTime, endTime time.Time, opts ...v1.Option) ([]string, v1.Warnings, error) {
	return nil, nil, nil
}
func (m *mockPromAPI) LabelValues(ctx context.Context, label string, matches []string, startTime, endTime time.Time, opts ...v1.Option) (model.LabelValues, v1.Warnings, error) {
	return nil, nil, nil
}
func (m *mockPromAPI) QueryExemplars(ctx context.Context, match string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	return nil, nil
}
func (m *mockPromAPI) Buildinfo(ctx context.Context) (v1.BuildinfoResult, error) {
	return v1.BuildinfoResult{}, nil
}
func (m *mockPromAPI) Runtimeinfo(ctx context.Context) (v1.RuntimeinfoResult, error) {
	return v1.RuntimeinfoResult{}, nil
}
func (m *mockPromAPI) Series(ctx context.Context, matches []string, startTime, endTime time.Time, opts ...v1.Option) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, nil
}
func (m *mockPromAPI) Snapshot(ctx context.Context, skipHead bool) (v1.SnapshotResult, error) {
	return v1.SnapshotResult{}, nil
}
func (m *mockPromAPI) Rules(ctx context.Context) (v1.RulesResult, error) {
	return v1.RulesResult{}, nil
}
func (m *mockPromAPI) Targets(ctx context.Context) (v1.TargetsResult, error) {
	return v1.TargetsResult{}, nil
}
func (m *mockPromAPI) TargetsMetadata(ctx context.Context, matchTarget, metric, limit string) ([]v1.MetricMetadata, error) {
	return nil, nil
}
func (m *mockPromAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	return nil, nil
}
func (m *mockPromAPI) TSDB(ctx context.Context, opts ...v1.Option) (v1.TSDBResult, error) {
	return v1.TSDBResult{}, nil
}
func (m *mockPromAPI) WalReplay(ctx context.Context) (v1.WalReplayStatus, error) {
	return v1.WalReplayStatus{}, nil
}

var _ v1.API = (*mockPromAPI)(nil)

// containsErr reports whether the error message contains the given substring.
// PrometheusMetricsClient wraps errors with fmt.Errorf (no %w), so errors.Is
// cannot see the underlying sentinel.
func containsErr(err error, sub string) bool {
	return err != nil && strings.Contains(err.Error(), sub)
}

// newTestClient builds a PrometheusMetricsClient wired to the mock and, when
// dcgm is true, enables the DCGM defaults that NewPrometheusMetricsClient sets.
func newTestClient(api v1.API, dcgm bool) *PrometheusMetricsClient {
	c := &PrometheusMetricsClient{
		client:        api,
		queryTimeout:  30 * time.Second,
		metricsPrefix: "compute_gardener_gpu",
		useDCGM:       dcgm,
	}
	if dcgm {
		c.dcgmPowerMetric = "DCGM_FI_DEV_POWER_USAGE"
		c.dcgmUtilMetric = "DCGM_FI_DEV_GPU_UTIL"
	}
	return c
}

func TestSettersAndGetters(t *testing.T) {
	c := newTestClient(&mockPromAPI{}, true)

	c.SetUseDCGM(false)
	if c.useDCGM {
		t.Fatal("expected useDCGM to be false after SetUseDCGM(false)")
	}
	c.SetUseDCGM(true)
	if !c.useDCGM {
		t.Fatal("expected useDCGM to be true after SetUseDCGM(true)")
	}

	c.SetDCGMPowerMetric("PWR")
	if got := c.GetDCGMPowerMetric(); got != "PWR" {
		t.Fatalf("GetDCGMPowerMetric = %q, want PWR", got)
	}

	c.SetDCGMUtilMetric("UTIL")
	if got := c.GetDCGMUtilMetric(); got != "UTIL" {
		t.Fatalf("GetDCGMUtilMetric = %q, want UTIL", got)
	}
}

func TestNewPrometheusMetricsClient(t *testing.T) {
	// A well-formed URL produces a client with DCGM defaults enabled.
	c, err := NewPrometheusMetricsClient("http://127.0.0.1:9090")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.useDCGM {
		t.Fatal("expected DCGM enabled by default")
	}
	if c.dcgmPowerMetric == "" || c.dcgmUtilMetric == "" {
		t.Fatal("expected default DCGM metric names to be set")
	}
	if c.queryTimeout <= 0 {
		t.Fatal("expected a positive query timeout")
	}
}

func TestNewPrometheusMetricsClientInvalidURL(t *testing.T) {
	// A URL that url.Parse rejects surfaces as an error.
	if _, err := NewPrometheusMetricsClient("://bad"); err == nil {
		t.Fatal("expected an error for an invalid URL")
	}
}

func TestNewLegacyPrometheusMetricsClient(t *testing.T) {
	c, err := NewLegacyPrometheusMetricsClient("http://127.0.0.1:9090")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.useDCGM {
		t.Fatal("expected legacy client to have DCGM disabled")
	}
	if c.queryTimeout <= 0 {
		t.Fatal("expected a positive query timeout")
	}
}

func TestNewLegacyPrometheusMetricsClientInvalidURL(t *testing.T) {
	if _, err := NewLegacyPrometheusMetricsClient("://bad"); err == nil {
		t.Fatal("expected an error for an invalid URL")
	}
}

func TestGetNodeCPUTemperature(t *testing.T) {
	api := &mockPromAPI{
		queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
			return model.Vector{
				{Metric: model.Metric{}, Value: model.SampleValue(63.5), Timestamp: model.Now()},
			}, nil, nil
		},
	}
	c := newTestClient(api, false)

	got, err := c.GetNodeCPUTemperature(context.Background(), "node-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 63.5 {
		t.Fatalf("got %v, want 63.5", got)
	}
}

func TestGetNodeGPUTemperature(t *testing.T) {
	t.Run("DCGM disabled returns error", func(t *testing.T) {
		c := newTestClient(&mockPromAPI{}, false)
		if _, err := c.GetNodeGPUTemperature(context.Background(), "node-1", "core"); err == nil {
			t.Fatal("expected error when DCGM is disabled")
		}
	})

	t.Run("unsupported type returns error", func(t *testing.T) {
		c := newTestClient(&mockPromAPI{}, true)
		if _, err := c.GetNodeGPUTemperature(context.Background(), "node-1", "bogus"); err == nil {
			t.Fatal("expected error for unsupported temp type")
		}
	})

	t.Run("core metric", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{{Value: model.SampleValue(67.0)}}, nil, nil
			},
		}
		c := newTestClient(api, true)
		got, err := c.GetNodeGPUTemperature(context.Background(), "node-1", "core")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 67.0 {
			t.Fatalf("expected 67.0, got %v", got)
		}
		if api.lastQuery == "" {
			t.Fatal("expected a query to be issued")
		}
	})

	t.Run("memory metric", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{{Value: model.SampleValue(71.0)}}, nil, nil
			},
		}
		c := newTestClient(api, true)
		got, err := c.GetNodeGPUTemperature(context.Background(), "node-1", "memory")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 71.0 {
			t.Fatalf("got %v, want 71.0", got)
		}
	})
}

func TestQueryNodeMetric(t *testing.T) {
	t.Run("vector returns first value", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{{Value: model.SampleValue(42.0)}}, nil, nil
			},
		}
		c := newTestClient(api, false)
		got, err := c.QueryNodeMetric(context.Background(), "metric", "node-1")
		if err != nil || got != 42.0 {
			t.Fatalf("got %v, %v; want 42.0, nil", got, err)
		}
	})

	t.Run("empty vector returns error", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{}, nil, nil
			},
		}
		c := newTestClient(api, false)
		if _, err := c.QueryNodeMetric(context.Background(), "metric", "node-1"); err == nil {
			t.Fatal("expected error for empty vector")
		}
	})

	t.Run("query error propagates", func(t *testing.T) {
		sentinel := errors.New("query failed")
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return nil, nil, sentinel
			},
		}
		c := newTestClient(api, false)
		if _, err := c.QueryNodeMetric(context.Background(), "metric", "node-1"); !containsErr(err, "query failed") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("unexpected result type returns error", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return &model.Scalar{Value: 1.0, Timestamp: model.Now()}, nil, nil
			},
		}
		c := newTestClient(api, false)
		if _, err := c.QueryNodeMetric(context.Background(), "metric", "node-1"); err == nil {
			t.Fatal("expected error for non-vector result")
		}
	})

	t.Run("warnings are tolerated", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{{Value: model.SampleValue(1.0)}}, v1.Warnings{"some warning"}, nil
			},
		}
		c := newTestClient(api, false)
		if _, err := c.QueryNodeMetric(context.Background(), "metric", "node-1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGetPodGPUUtilization(t *testing.T) {
	t.Run("DCGM path", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				// 75% -> 0.75
				return model.Vector{{Value: model.SampleValue(75.0)}}, nil, nil
			},
		}
		c := newTestClient(api, true)
		got, err := c.GetPodGPUUtilization(context.Background(), "ns", "pod")
		if err != nil || got != 0.75 {
			t.Fatalf("got %v, %v; want 0.75, nil", got, err)
		}
	})

	t.Run("legacy path", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{{Value: model.SampleValue(50.0)}}, nil, nil
			},
		}
		c := newTestClient(api, false)
		got, err := c.GetPodGPUUtilization(context.Background(), "ns", "pod")
		if err != nil || got != 0.5 {
			t.Fatalf("got %v, %v; want 0.5, nil", got, err)
		}
	})

	t.Run("empty vector returns 0, nil", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{}, nil, nil
			},
		}
		c := newTestClient(api, true)
		got, err := c.GetPodGPUUtilization(context.Background(), "ns", "pod")
		if err != nil || got != 0 {
			t.Fatalf("got %v, %v; want 0, nil", got, err)
		}
	})

	t.Run("query error propagates", func(t *testing.T) {
		sentinel := errors.New("boom")
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return nil, nil, sentinel
			},
		}
		c := newTestClient(api, true)
		if _, err := c.GetPodGPUUtilization(context.Background(), "ns", "pod"); !containsErr(err, "boom") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("unexpected type returns error", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return &model.Scalar{}, nil, nil
			},
		}
		c := newTestClient(api, true)
		if _, err := c.GetPodGPUUtilization(context.Background(), "ns", "pod"); err == nil {
			t.Fatal("expected error for non-vector result")
		}
	})
}

func TestListPodsGPUUtilization(t *testing.T) {
	t.Run("DCGM by UUID", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{
					{Metric: model.Metric{"UUID": "uuid-1"}, Value: model.SampleValue(80.0)},
					{Metric: model.Metric{}, Value: model.SampleValue(10.0)}, // missing UUID -> skipped
				}, nil, nil
			},
		}
		c := newTestClient(api, true)
		got, err := c.ListPodsGPUUtilization(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got["gpu/uuid-1"] != 0.8 {
			t.Fatalf("unexpected result: %+v", got)
		}
	})

	t.Run("legacy by pod/namespace", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{
					{Metric: model.Metric{"pod": "p1", "namespace": "ns1"}, Value: model.SampleValue(40.0)},
					{Metric: model.Metric{"pod": "p2"}, Value: model.SampleValue(10.0)}, // missing ns -> skipped
				}, nil, nil
			},
		}
		c := newTestClient(api, false)
		got, err := c.ListPodsGPUUtilization(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got["ns1/p1"] != 0.4 {
			t.Fatalf("unexpected result: %+v", got)
		}
	})

	t.Run("query error propagates", func(t *testing.T) {
		sentinel := errors.New("boom")
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return nil, nil, sentinel
			},
		}
		c := newTestClient(api, true)
		if _, err := c.ListPodsGPUUtilization(context.Background()); !containsErr(err, "boom") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})
}

func TestGetPodGPUPower(t *testing.T) {
	t.Run("DCGM path", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{{Value: model.SampleValue(220.0)}}, nil, nil
			},
		}
		c := newTestClient(api, true)
		got, err := c.GetPodGPUPower(context.Background(), "ns", "pod")
		if err != nil || got != 220.0 {
			t.Fatalf("got %v, %v; want 220.0, nil", got, err)
		}
	})

	t.Run("empty vector returns 0, nil", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{}, nil, nil
			},
		}
		c := newTestClient(api, false)
		got, err := c.GetPodGPUPower(context.Background(), "ns", "pod")
		if err != nil || got != 0 {
			t.Fatalf("got %v, %v; want 0, nil", got, err)
		}
	})

	t.Run("query error propagates", func(t *testing.T) {
		sentinel := errors.New("boom")
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return nil, nil, sentinel
			},
		}
		c := newTestClient(api, true)
		if _, err := c.GetPodGPUPower(context.Background(), "ns", "pod"); !containsErr(err, "boom") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("unexpected type returns error", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return &model.Scalar{}, nil, nil
			},
		}
		c := newTestClient(api, true)
		if _, err := c.GetPodGPUPower(context.Background(), "ns", "pod"); err == nil {
			t.Fatal("expected error for non-vector result")
		}
	})
}

func TestListPodsGPUPower(t *testing.T) {
	t.Run("DCGM by UUID", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{
					{Metric: model.Metric{"UUID": "uuid-1"}, Value: model.SampleValue(150.0)},
					{Metric: model.Metric{}, Value: model.SampleValue(10.0)}, // missing UUID -> skipped
				}, nil, nil
			},
		}
		c := newTestClient(api, true)
		got, err := c.ListPodsGPUPower(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got["gpu/uuid-1"] != 150.0 {
			t.Fatalf("unexpected result: %+v", got)
		}
	})

	t.Run("legacy by pod/namespace", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{
					{Metric: model.Metric{"pod": "p1", "namespace": "ns1"}, Value: model.SampleValue(90.0)},
					{Metric: model.Metric{"namespace": "ns1"}, Value: model.SampleValue(10.0)}, // missing pod -> skipped
				}, nil, nil
			},
		}
		c := newTestClient(api, false)
		got, err := c.ListPodsGPUPower(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got["ns1/p1"] != 90.0 {
			t.Fatalf("unexpected result: %+v", got)
		}
	})

	t.Run("query error propagates", func(t *testing.T) {
		sentinel := errors.New("boom")
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return nil, nil, sentinel
			},
		}
		c := newTestClient(api, true)
		if _, err := c.ListPodsGPUPower(context.Background()); !containsErr(err, "boom") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})
}

func TestGetPodHistoricalGPUMetrics(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// A matrix with one series and three points for both util and power.
	utilMatrix := model.Matrix{
		{
			Metric: model.Metric{},
			Values: []model.SamplePair{
				{Timestamp: model.TimeFromUnix(start.Unix()), Value: model.SampleValue(10.0)},
				{Timestamp: model.TimeFromUnix(start.Add(15 * time.Second).Unix()), Value: model.SampleValue(20.0)},
				{Timestamp: model.TimeFromUnix(start.Add(30 * time.Second).Unix()), Value: model.SampleValue(30.0)},
			},
		},
	}

	t.Run("DCGM happy path with step sizing", func(t *testing.T) {
		api := &mockPromAPI{
			queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				// Range is ~30s, so step should be 15s.
				if r.Step != 15*time.Second {
					t.Errorf("expected step 15s, got %v", r.Step)
				}
				return utilMatrix, nil, nil
			},
		}
		c := newTestClient(api, true)
		h, err := c.GetPodHistoricalGPUMetrics(context.Background(), "ns", "pod", start, start.Add(30*time.Second))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(h.Timestamps) != 3 || len(h.Utilization) != 3 {
			t.Fatalf("expected 3 data points, got %d timestamps / %d util", len(h.Timestamps), len(h.Utilization))
		}
		if h.PodName != "pod" || h.Namespace != "ns" {
			t.Fatalf("unexpected pod/namespace: %s/%s", h.Namespace, h.PodName)
		}
	})

	t.Run("power-only (no util) creates timestamps", func(t *testing.T) {
		// The "power-only creates timestamps" branch is exercised when util is
		// empty and power is non-empty. The util and power queries carry
		// different metric names, so use that to decide which to fill.
		utilEmpty := model.Matrix{}
		api := &mockPromAPI{
			queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				if q == "avg(DCGM_FI_DEV_GPU_UTIL)" {
					return utilEmpty, nil, nil
				}
				return utilMatrix, nil, nil
			},
		}
		c := newTestClient(api, true)
		h, err := c.GetPodHistoricalGPUMetrics(context.Background(), "ns", "pod", start, start.Add(30*time.Second))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Util empty, power non-empty -> timestamps derived from power.
		if len(h.Timestamps) != 3 || len(h.Power) != 3 {
			t.Fatalf("expected 3 timestamps and 3 power values, got %d / %d", len(h.Timestamps), len(h.Power))
		}
	})

	t.Run("range error on util propagates", func(t *testing.T) {
		sentinel := errors.New("util boom")
		api := &mockPromAPI{
			queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return nil, nil, sentinel
			},
		}
		c := newTestClient(api, true)
		if _, err := c.GetPodHistoricalGPUMetrics(context.Background(), "ns", "pod", start, start.Add(30*time.Second)); !containsErr(err, "boom") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("range error on power propagates", func(t *testing.T) {
		sentinel := errors.New("power boom")
		api := &mockPromAPI{
			queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				if q == "avg(DCGM_FI_DEV_GPU_UTIL)" {
					return utilMatrix, nil, nil // util ok
				}
				return nil, nil, sentinel // power fails
			},
		}
		c := newTestClient(api, true)
		if _, err := c.GetPodHistoricalGPUMetrics(context.Background(), "ns", "pod", start, start.Add(30*time.Second)); !containsErr(err, "boom") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("no data logs and returns empty history", func(t *testing.T) {
		api := &mockPromAPI{
			queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Matrix{}, nil, nil
			},
		}
		c := newTestClient(api, true)
		h, err := c.GetPodHistoricalGPUMetrics(context.Background(), "ns", "pod", start, start.Add(30*time.Second))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(h.Timestamps) != 0 {
			t.Fatalf("expected empty history, got %d timestamps", len(h.Timestamps))
		}
	})
}

func TestCalculateAverageGPUMetrics(t *testing.T) {
	t.Run("empty returns zeros", func(t *testing.T) {
		h := &PodGPUMetricsHistory{}
		u, p := h.CalculateAverageGPUMetrics()
		if u != 0 || p != 0 {
			t.Fatalf("expected 0,0 got %v,%v", u, p)
		}
	})

	t.Run("computes averages", func(t *testing.T) {
		h := &PodGPUMetricsHistory{
			Timestamps:  []time.Time{time.Now(), time.Now()},
			Utilization: []float64{10.0, 20.0},
			Power:       []float64{100.0, 200.0},
		}
		u, p := h.CalculateAverageGPUMetrics()
		if u != 15.0 || p != 150.0 {
			t.Fatalf("expected util 15.0 / power 150.0, got %v / %v", u, p)
		}
	})
}

func TestCalculateTotalEnergy(t *testing.T) {
	t.Run("fewer than two points returns 0", func(t *testing.T) {
		h := &PodGPUMetricsHistory{
			Timestamps: []time.Time{time.Now()},
			Power:      []float64{100.0},
		}
		if e := h.CalculateTotalEnergy(); e != 0 {
			t.Fatalf("expected 0, got %v", e)
		}
	})

	t.Run("trapezoid integration", func(t *testing.T) {
		t0 := time.Now()
		t1 := t0.Add(1 * time.Hour)
		h := &PodGPUMetricsHistory{
			Timestamps: []time.Time{t0, t1},
			Power:      []float64{100.0, 200.0},
		}
		// avg power = 150W over 1h -> 150 Wh
		if e := h.CalculateTotalEnergy(); e != 150.0 {
			t.Fatalf("expected 150.0 Wh, got %v", e)
		}
	})
}

func TestQueryGPUInstanceLabels(t *testing.T) {
	t.Run("DCGM disabled returns error", func(t *testing.T) {
		c := newTestClient(&mockPromAPI{}, false)
		if _, err := c.QueryGPUInstanceLabels(context.Background(), fake.NewSimpleClientset()); err == nil {
			t.Fatal("expected error when DCGM is disabled")
		}
	})

	t.Run("query error propagates", func(t *testing.T) {
		sentinel := errors.New("boom")
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return nil, nil, sentinel
			},
		}
		c := newTestClient(api, true)
		if _, err := c.QueryGPUInstanceLabels(context.Background(), fake.NewSimpleClientset()); !containsErr(err, "boom") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("maps UUID to node via DCGM exporter pod", func(t *testing.T) {
		api := &mockPromAPI{
			queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{
					{Metric: model.Metric{"UUID": "uuid-1", "pod": "dcgm-exporter-abc", "namespace": "gpu"}, Value: model.SampleValue(100.0)},
					{Metric: model.Metric{"UUID": "uuid-2", "pod": "dcgm-exporter-def"}, Value: model.SampleValue(100.0)},                     // missing namespace -> skipped
					{Metric: model.Metric{"UUID": "uuid-3", "pod": "dcgm-exporter-xyz", "namespace": "gpu"}, Value: model.SampleValue(100.0)}, // pod not found -> skipped
				}, nil, nil
			},
		}
		kube := fake.NewSimpleClientset(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "dcgm-exporter-abc", Namespace: "gpu"},
			Spec:       corev1.PodSpec{NodeName: "node-1"},
		})
		c := newTestClient(api, true)
		got, err := c.QueryGPUInstanceLabels(context.Background(), kube)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got["uuid-1"] != "node-1" {
			t.Fatalf("unexpected mapping: %+v", got)
		}
	})
}

func TestResolvePodToNodeName(t *testing.T) {
	t.Run("returns node name", func(t *testing.T) {
		kube := fake.NewSimpleClientset(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "ns"},
			Spec:       corev1.PodSpec{NodeName: "node-9"},
		})
		c := newTestClient(&mockPromAPI{}, true)
		got, err := c.resolvePodToNodeName(context.Background(), kube, "pod-1", "ns")
		if err != nil || got != "node-9" {
			t.Fatalf("got %q, %v; want node-9, nil", got, err)
		}
	})

	t.Run("pod not found returns error", func(t *testing.T) {
		kube := fake.NewSimpleClientset()
		c := newTestClient(&mockPromAPI{}, true)
		if _, err := c.resolvePodToNodeName(context.Background(), kube, "missing", "ns"); err == nil {
			t.Fatal("expected error for missing pod")
		}
	})

	t.Run("pod without node returns error", func(t *testing.T) {
		kube := fake.NewSimpleClientset(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "ns"},
			Spec:       corev1.PodSpec{},
		})
		c := newTestClient(&mockPromAPI{}, true)
		if _, err := c.resolvePodToNodeName(context.Background(), kube, "pod-1", "ns"); err == nil {
			t.Fatal("expected error for pod without node")
		}
	})
}

func TestQueryHistoricalCarbonIntensity(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("matrix returns points", func(t *testing.T) {
		api := &mockPromAPI{
			queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Matrix{
					{
						Metric: model.Metric{},
						Values: []model.SamplePair{
							{Timestamp: model.TimeFromUnix(start.Unix()), Value: model.SampleValue(12.5)},
							{Timestamp: model.TimeFromUnix(start.Add(15 * time.Second).Unix()), Value: model.SampleValue(22.5)},
						},
					},
				}, nil, nil
			},
		}
		c := newTestClient(api, false)
		points, err := c.QueryHistoricalCarbonIntensity(context.Background(), "us-east-1", start, start.Add(30*time.Second), 15*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(points) != 2 || points[0].Intensity != 12.5 || points[1].Intensity != 22.5 {
			t.Fatalf("unexpected points: %+v", points)
		}
	})

	t.Run("range error propagates", func(t *testing.T) {
		sentinel := errors.New("boom")
		api := &mockPromAPI{
			queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return nil, nil, sentinel
			},
		}
		c := newTestClient(api, false)
		if _, err := c.QueryHistoricalCarbonIntensity(context.Background(), "region", start, start.Add(time.Hour), time.Minute); !containsErr(err, "boom") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("non-matrix type returns error", func(t *testing.T) {
		api := &mockPromAPI{
			queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Vector{}, nil, nil
			},
		}
		c := newTestClient(api, false)
		if _, err := c.QueryHistoricalCarbonIntensity(context.Background(), "region", start, start.Add(time.Hour), time.Minute); err == nil {
			t.Fatal("expected error for non-matrix result")
		}
	})

	t.Run("empty matrix returns error", func(t *testing.T) {
		api := &mockPromAPI{
			queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
				return model.Matrix{}, nil, nil
			},
		}
		c := newTestClient(api, false)
		if _, err := c.QueryHistoricalCarbonIntensity(context.Background(), "region", start, start.Add(time.Hour), time.Minute); err == nil {
			t.Fatal("expected error for empty matrix")
		}
	})
}

// TestWarningsToleredAcrossMethods exercises the "warnings are logged" branch
// in every method that surfaces them.
func TestWarningsToleredAcrossMethods(t *testing.T) {
	warn := v1.Warnings{"degraded"}
	vec := model.Vector{{Value: model.SampleValue(1.0)}}
	mat := model.Matrix{{Values: []model.SamplePair{{Value: model.SampleValue(1.0)}}}}

	api := &mockPromAPI{
		queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
			return vec, warn, nil
		},
		queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
			return mat, warn, nil
		},
	}
	c := newTestClient(api, true)
	ctx := context.Background()

	if _, err := c.GetPodGPUUtilization(ctx, "ns", "pod"); err != nil {
		t.Fatalf("GetPodGPUUtilization: %v", err)
	}
	if _, err := c.ListPodsGPUUtilization(ctx); err != nil {
		t.Fatalf("ListPodsGPUUtilization: %v", err)
	}
	if _, err := c.GetPodGPUPower(ctx, "ns", "pod"); err != nil {
		t.Fatalf("GetPodGPUPower: %v", err)
	}
	if _, err := c.ListPodsGPUPower(ctx); err != nil {
		t.Fatalf("ListPodsGPUPower: %v", err)
	}
	start := time.Now()
	if _, err := c.GetPodHistoricalGPUMetrics(ctx, "ns", "pod", start, start.Add(15*time.Second)); err != nil {
		t.Fatalf("GetPodHistoricalGPUMetrics: %v", err)
	}
	if _, err := c.QueryHistoricalCarbonIntensity(ctx, "region", start, start.Add(15*time.Second), 5*time.Second); err != nil {
		t.Fatalf("QueryHistoricalCarbonIntensity: %v", err)
	}
	if _, err := c.QueryGPUInstanceLabels(ctx, fake.NewSimpleClientset()); err != nil {
		t.Fatalf("QueryGPUInstanceLabels: %v", err)
	}
}

// TestGetPodHistoricalGPUMetricsStepSizing verifies the step chosen for each
// time-range bucket (15s, 1m, 5m).
func TestGetPodHistoricalGPUMetricsStepSizing(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		span time.Duration
		step time.Duration
	}{
		{"short range uses 15s", 30 * time.Second, 15 * time.Second},
		{"medium range uses 1m", time.Hour, time.Minute},
		{"long range uses 5m", 4 * time.Hour, 5 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotStep time.Duration
			api := &mockPromAPI{
				queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
					gotStep = r.Step
					return model.Matrix{}, nil, nil
				},
			}
			c := newTestClient(api, true)
			if _, err := c.GetPodHistoricalGPUMetrics(context.Background(), "ns", "pod", start, start.Add(tc.span)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotStep != tc.step {
				t.Fatalf("step = %v, want %v", gotStep, tc.step)
			}
		})
	}
}

// TestGetPodHistoricalGPUMetricsLegacy exercises the non-DCGM query-building
// branch.
func TestGetPodHistoricalGPUMetricsLegacy(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	api := &mockPromAPI{
		queryRange: func(ctx context.Context, q string, r v1.Range, opts ...v1.Option) (model.Value, v1.Warnings, error) {
			return model.Matrix{
				{Values: []model.SamplePair{{Value: model.SampleValue(5.0)}}},
			}, nil, nil
		},
	}
	c := newTestClient(api, false)
	h, err := c.GetPodHistoricalGPUMetrics(context.Background(), "ns", "pod", start, start.Add(30*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h.Utilization) != 1 {
		t.Fatalf("expected 1 utilization point, got %d", len(h.Utilization))
	}
}

// TestQueryGPUInstanceLabelsMissingUUID covers the missing-UUID skip branch
// and its label-dump logging closure.
func TestQueryGPUInstanceLabelsMissingUUID(t *testing.T) {
	api := &mockPromAPI{
		queryFn: func(ctx context.Context, q string, ts time.Time, opts ...v1.Option) (model.Value, v1.Warnings, error) {
			return model.Vector{
				{Metric: model.Metric{"pod": "p", "namespace": "ns"}, Value: model.SampleValue(1.0)}, // no UUID
			}, nil, nil
		},
	}
	c := newTestClient(api, true)
	got, err := c.QueryGPUInstanceLabels(context.Background(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty mapping, got %+v", got)
	}
}
