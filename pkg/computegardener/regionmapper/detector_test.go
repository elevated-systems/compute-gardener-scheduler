package regionmapper

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDetectAWSRegion(t *testing.T) {
	testCases := []struct {
		name         string
		node         *v1.Node
		expectOk     bool
		expectRegion string
	}{
		{
			name: "from providerID",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "aws://us-east-1/i-12345",
				},
			},
			expectOk:     true,
			expectRegion: "us-east-1",
		},
		{
			name: "from beta region label",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"failure-domain.beta.kubernetes.io/region": "us-west-2",
					},
				},
			},
			expectOk:     true,
			expectRegion: "us-west-2",
		},
		{
			name: "from zone label",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"topology.kubernetes.io/zone": "us-east-1a",
					},
				},
			},
			expectOk:     true,
			expectRegion: "us-east-1",
		},
		{
			name: "invalid providerID format",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "aws://invalid-format",
				},
			},
			expectOk:     false,
			expectRegion: "",
		},
		{
			name: "invalid zone format",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"topology.kubernetes.io/zone": "a", // Too short to extract region
					},
				},
			},
			expectOk:     false,
			expectRegion: "",
		},
		{
			name:         "no AWS metadata",
			node:         &v1.Node{},
			expectOk:     false,
			expectRegion: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			region, ok := detectAWSRegion(tc.node)

			if ok != tc.expectOk {
				t.Errorf("Expected ok=%v, got %v", tc.expectOk, ok)
			}

			if region != tc.expectRegion {
				t.Errorf("Expected region %q, got %q", tc.expectRegion, region)
			}
		})
	}
}

func TestDetectGCPRegion(t *testing.T) {
	testCases := []struct {
		name         string
		node         *v1.Node
		expectOk     bool
		expectRegion string
	}{
		{
			name: "from providerID",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "gce://project-id/us-central1-a/vm-1",
				},
			},
			expectOk:     true,
			expectRegion: "us-central1",
		},
		{
			name: "from zone label",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"topology.kubernetes.io/zone": "us-west1-b",
					},
				},
			},
			expectOk:     true,
			expectRegion: "us-west1",
		},
		{
			name: "from invalid providerID format",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "gce://invalid-format",
				},
			},
			expectOk:     false,
			expectRegion: "",
		},
		{
			name: "from zone with no dash",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"topology.kubernetes.io/zone": "uswest1", // No dash to split on
					},
				},
			},
			expectOk:     true,
			expectRegion: "uswest1", // Returns the full zone when can't extract region
		},
		{
			name:         "no GCP metadata",
			node:         &v1.Node{},
			expectOk:     false,
			expectRegion: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			region, ok := detectGCPRegion(tc.node)

			if ok != tc.expectOk {
				t.Errorf("Expected ok=%v, got %v", tc.expectOk, ok)
			}

			if region != tc.expectRegion {
				t.Errorf("Expected region %q, got %q", tc.expectRegion, region)
			}
		})
	}
}

func TestDetectAzureRegion(t *testing.T) {
	testCases := []struct {
		name         string
		node         *v1.Node
		expectOk     bool
		expectRegion string
	}{
		{
			name: "from beta region label",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"failure-domain.beta.kubernetes.io/region": "eastus",
					},
				},
			},
			expectOk:     true,
			expectRegion: "eastus",
		},
		{
			name: "from azure location label",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"kubernetes.azure.com/location": "westus2",
					},
				},
			},
			expectOk:     true,
			expectRegion: "westus2",
		},
		{
			name: "with providerID but no region labels",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "azure:///subscriptions/sub-id/resourceGroups/resource-group/providers/Microsoft.Compute/virtualMachines/node-name",
				},
			},
			expectOk:     false,
			expectRegion: "",
		},
		{
			name:         "no Azure metadata",
			node:         &v1.Node{},
			expectOk:     false,
			expectRegion: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			region, ok := detectAzureRegion(tc.node)

			if ok != tc.expectOk {
				t.Errorf("Expected ok=%v, got %v", tc.expectOk, ok)
			}

			if region != tc.expectRegion {
				t.Errorf("Expected region %q, got %q", tc.expectRegion, region)
			}
		})
	}
}
