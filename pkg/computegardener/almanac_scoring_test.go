package computegardener

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/almanac"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/carbon"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/price"
	testingmocks "github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/testing"
)

// newAlmanacScheduler builds a test scheduler where almanac is enabled at the
// scheduler level, so podAlmanacEnabled() hinges only on the pod annotation.
func newAlmanacScheduler(cfg *config.Config, carbonImpl carbon.Implementation, priceImpl price.Implementation, baseTime time.Time) *ComputeGardenerScheduler {
	s := newTestSchedulerWithCustomClients(cfg, nil, nil, carbonImpl, priceImpl, 0.1, baseTime, nil)
	s.almanacClient = almanac.NewClient("http://almanac.test:8080")
	return s
}

// TestPodAlmanacEnabled verifies the feature gate + per-pod opt-in logic.
func TestPodAlmanacEnabled(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	mkScheduler := func(enabled bool) *ComputeGardenerScheduler {
		cfg := &config.Config{}
		if enabled {
			cfg.Almanac = config.AlmanacConfig{Enabled: true}
		}
		cs := newTestScheduler(cfg, 0, 0, baseTime)
		cs.almanacClient = almanac.NewClient("http://almanac.test:8080")
		return cs
	}

	podWith := func(v string) *v1.Pod {
		p := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "uid"}}
		if v != "" {
			p.Annotations = map[string]string{common.AnnotationAlmanacEnabled: v}
		}
		return p
	}

	tests := []struct {
		name           string
		featureEnabled bool
		annotation     string
		want           bool
	}{
		{"feature off, no annotation", false, "", false},
		{"feature off, annotation true", false, "true", false},
		{"feature on, no annotation (must opt in)", true, "", false},
		{"feature on, annotation true", true, "true", true},
		{"feature on, annotation false", true, "false", false},
		{"feature on, annotation garbage", true, "notabool", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := mkScheduler(tt.featureEnabled)
			if got := cs.podAlmanacEnabled(podWith(tt.annotation)); got != tt.want {
				t.Errorf("podAlmanacEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPrefilterSkipsCarbonWhenAlmanacEnabled verifies the blended almanac score
// takes precedence over the local carbon intensity check: a pod that would be
// delayed by the local carbon threshold is allowed through PreFilter when
// almanac is active for it.
func TestPrefilterSkipsCarbonWhenAlmanacEnabled(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		Carbon: config.CarbonConfig{
			Enabled:            true,
			IntensityThreshold: 0.5,
		},
		Almanac: config.AlmanacConfig{Enabled: true},
	}

	// Mock carbon at 0.9 > threshold 0.5, so the LOCAL check would fail.
	hotCarbon := testingmocks.NewMockCarbon(0.9)

	cs := newAlmanacScheduler(cfg, hotCarbon, nil, baseTime)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "p", Namespace: "default", UID: "uid",
			Annotations: map[string]string{common.AnnotationAlmanacEnabled: "true"},
		},
		Spec: v1.PodSpec{SchedulerName: common.SchedulerName},
	}

	_, status := cs.PreFilter(context.Background(), framework.NewCycleState(), pod)
	if !status.IsSuccess() {
		t.Errorf("PreFilter should skip the local carbon check when almanac is active; got: %v", status)
	}
}

// TestPrefilterAppliesCarbonWhenAlmanacDisabled verifies the regression path:
// without the almanac opt-in, the local carbon threshold still delays the pod.
func TestPrefilterAppliesCarbonWhenAlmanacDisabled(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		Carbon: config.CarbonConfig{
			Enabled:            true,
			IntensityThreshold: 0.5,
		},
		// Almanac enabled at scheduler level but pod does NOT opt in.
		Almanac: config.AlmanacConfig{Enabled: true},
	}

	hotCarbon := testingmocks.NewMockCarbon(0.9)
	cs := newAlmanacScheduler(cfg, hotCarbon, nil, baseTime)

	// No almanac annotation -> local carbon check applies and blocks the pod.
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "uid"},
		Spec:       v1.PodSpec{SchedulerName: common.SchedulerName},
	}

	_, status := cs.PreFilter(context.Background(), framework.NewCycleState(), pod)
	if status.IsSuccess() {
		t.Errorf("PreFilter should apply the local carbon check and block the pod; got success")
	}
	if status.Code() != framework.Unschedulable {
		t.Errorf("expected Unschedulable, got %v", status.Code())
	}
}

// TestPrefilterSkipsPriceWhenAlmanacEnabled verifies the local TOU price check
// is bypassed when almanac is active for the pod.
func TestPrefilterSkipsPriceWhenAlmanacEnabled(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		Pricing: config.PriceConfig{Enabled: true},
		Almanac: config.AlmanacConfig{Enabled: true},
	}

	// Mock price at 0.2 > default threshold 0.15, so the LOCAL check would fail.
	peakPrice := &testingmocks.MockPriceImplementation{
		GetCurrentRateFunc: func(time.Time) float64 { return 0.2 },
		IsPeakTimeFunc:     func(time.Time) bool { return true },
	}

	cs := newAlmanacScheduler(cfg, nil, peakPrice, baseTime)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "p", Namespace: "default", UID: "uid",
			Annotations: map[string]string{common.AnnotationAlmanacEnabled: "true"},
		},
		Spec: v1.PodSpec{SchedulerName: common.SchedulerName},
	}

	_, status := cs.PreFilter(context.Background(), framework.NewCycleState(), pod)
	if !status.IsSuccess() {
		t.Errorf("PreFilter should skip the local price check when almanac is active; got: %v", status)
	}
}

// TestPrefilterAppliesPriceWhenAlmanacDisabled verifies the regression path for
// price: without almanac opt-in, a pod above the price threshold is delayed.
func TestPrefilterAppliesPriceWhenAlmanacDisabled(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		Pricing: config.PriceConfig{Enabled: true},
		Almanac: config.AlmanacConfig{Enabled: true},
	}

	peakPrice := &testingmocks.MockPriceImplementation{
		GetCurrentRateFunc: func(time.Time) float64 { return 0.2 },
		IsPeakTimeFunc:     func(time.Time) bool { return true },
	}

	cs := newAlmanacScheduler(cfg, nil, peakPrice, baseTime)

	// No almanac annotation -> local price check applies and blocks the pod.
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "uid"},
		Spec:       v1.PodSpec{SchedulerName: common.SchedulerName},
	}

	_, status := cs.PreFilter(context.Background(), framework.NewCycleState(), pod)
	if status.IsSuccess() {
		t.Errorf("PreFilter should apply the local price check and block the pod; got success")
	}
	if status.Code() != framework.Unschedulable {
		t.Errorf("expected Unschedulable, got %v", status.Code())
	}
}

// newCheckAlmanacScheduler builds a scheduler wired to a test almanac HTTP
// server that always returns the given optimization score.
func newCheckAlmanacScheduler(t *testing.T, score float64, almanacCfg config.AlmanacConfig, node *v1.Node) *ComputeGardenerScheduler {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/score" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"zone":"z","optimizationScore":%v,"recommendation":"PROCEED","components":{"carbonScore":0.8,"priceScore":0.7},"timestamp":"2024-01-01T00:00:00Z"}`, score)
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{Almanac: almanacCfg}
	cs := newTestScheduler(cfg, 0, 0, time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	cs.almanacClient = almanac.NewClient(srv.URL)
	return cs
}

func testAlmanacNode() *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-1",
			Labels: map[string]string{common.LabelTopologyZone: "us-west-2a"},
		},
		Spec: v1.NodeSpec{ProviderID: "aws://us-west-2a/i-1234"},
	}
}

func testAlmanacPod() *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "p", Namespace: "default", UID: "uid",
			Annotations: map[string]string{common.AnnotationAlmanacEnabled: "true"},
		},
		Spec: v1.PodSpec{SchedulerName: common.SchedulerName},
	}
}

func TestCheckAlmanacScoreGate(t *testing.T) {
	node := testAlmanacNode()
	pod := testAlmanacPod()
	cfg := config.AlmanacConfig{
		Enabled:               true,
		DefaultCarbonWeight:   0.6,
		DefaultPriceWeight:    0.4,
		DefaultScoreThreshold: 0.7,
	}

	tests := []struct {
		name      string
		score     float64
		failOpen  bool
		wantBlock bool
	}{
		{"score above threshold proceeds", 0.85, true, false},
		{"score below threshold blocks", 0.5, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := newCheckAlmanacScheduler(t, tt.score, cfg, node)
			status := cs.checkAlmanacScore(context.Background(), pod, node)
			if tt.wantBlock {
				if status == nil || status.IsSuccess() {
					t.Errorf("expected block, got %v", status)
				}
			} else if status != nil && !status.IsSuccess() {
				t.Errorf("expected proceed, got %v", status)
			}
		})
	}
}

func TestCheckAlmanacScoreFailOpenOnAPIError(t *testing.T) {
	// Point the client at a dead server to force a request error.
	cfg := &config.Config{Almanac: config.AlmanacConfig{
		Enabled: true, FailOpen: true, DefaultCarbonWeight: 0.6, DefaultPriceWeight: 0.4,
	}}
	cs := newTestScheduler(cfg, 0, 0, time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	cs.almanacClient = almanac.NewClient("http://127.0.0.1:1") // closed port

	node := testAlmanacNode()
	pod := testAlmanacPod()

	status := cs.checkAlmanacScore(context.Background(), pod, node)
	if status != nil && !status.IsSuccess() {
		t.Errorf("fail-open should allow scheduling on API error; got %v", status)
	}
}

func TestCheckAlmanacScoreFailClosedOnAPIError(t *testing.T) {
	cfg := &config.Config{Almanac: config.AlmanacConfig{
		Enabled: true, FailOpen: false, DefaultCarbonWeight: 0.6, DefaultPriceWeight: 0.4,
	}}
	cs := newTestScheduler(cfg, 0, 0, time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	cs.almanacClient = almanac.NewClient("http://127.0.0.1:1") // closed port

	node := testAlmanacNode()
	pod := testAlmanacPod()

	status := cs.checkAlmanacScore(context.Background(), pod, node)
	if status == nil || status.IsSuccess() {
		t.Errorf("fail-closed should block on API error; got %v", status)
	}
}

func TestCheckAlmanacScoreSkipsWhenNotOptedIn(t *testing.T) {
	cfg := config.AlmanacConfig{
		Enabled: true, DefaultCarbonWeight: 0.6, DefaultPriceWeight: 0.4, DefaultScoreThreshold: 0.7,
	}
	cs := newCheckAlmanacScheduler(t, 0.1, cfg, testAlmanacNode())

	node := testAlmanacNode()
	// Pod without the almanac annotation -> check is skipped entirely.
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", UID: "uid"},
		Spec:       v1.PodSpec{SchedulerName: common.SchedulerName},
	}

	if status := cs.checkAlmanacScore(context.Background(), pod, node); status != nil && !status.IsSuccess() {
		t.Errorf("should skip almanac scoring for non-opted-in pod; got %v", status)
	}
}
