package regionmapper

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewRegionMapper(t *testing.T) {
	mapper := NewRegionMapper()
	if mapper == nil {
		t.Fatal("NewRegionMapper returned nil")
	}
}

func TestGetRegionInfo(t *testing.T) {
	mapper := NewRegionMapper()

	testCases := []struct {
		provider  string
		region    string
		expectOk  bool
		expectPUE float64
	}{
		{"aws", "us-east-1", true, 1.2},
		{"gcp", "us-west1", true, 1.09},
		{"azure", "eastus", true, 1.25},
		{"unknown", "region", false, 0},
		{"aws", "unknown-region", false, 0},
	}

	for _, tc := range testCases {
		info, ok := mapper.GetRegionInfo(tc.provider, tc.region)
		if ok != tc.expectOk {
			t.Errorf("GetRegionInfo(%s, %s) returned ok=%v, expected %v", tc.provider, tc.region, ok, tc.expectOk)
		}

		if tc.expectOk && (info == nil || info.DefaultPUE != tc.expectPUE) {
			expectedPUE := tc.expectPUE
			actualPUE := 0.0
			if info != nil {
				actualPUE = info.DefaultPUE
			}
			t.Errorf("GetRegionInfo(%s, %s) returned PUE=%v, expected %v", tc.provider, tc.region, actualPUE, expectedPUE)
		}
	}
}

func TestDetectCloudProvider(t *testing.T) {
	testCases := []struct {
		providerID     string
		labels         map[string]string
		expectProvider string
	}{
		{"aws://us-east-1/instance-id", nil, "aws"},
		{"gce://project-id/us-central1/instance-id", nil, "gcp"},
		{"azure://subscription/resource", nil, "azure"},
		{"", map[string]string{"node.kubernetes.io/instance-type": "m5.large"}, "aws"},
		{"", map[string]string{"cloud.google.com/gke-nodepool": "pool-1"}, "gcp"},
		{"", map[string]string{"kubernetes.azure.com/cluster": "cluster-1"}, "azure"},
		{"", map[string]string{"unrelated": "label"}, ""},
	}

	for _, tc := range testCases {
		node := &v1.Node{
			Spec: v1.NodeSpec{
				ProviderID: tc.providerID,
			},
			ObjectMeta: metav1.ObjectMeta{
				Labels: tc.labels,
			},
		}

		provider := DetectCloudProvider(node)
		if provider != tc.expectProvider {
			t.Errorf("DetectCloudProvider returned %q, expected %q for providerID=%q, labels=%v", provider, tc.expectProvider, tc.providerID, tc.labels)
		}
	}
}
