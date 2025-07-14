package regionmapper

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewRegionMapper(t *testing.T) {
	client := fake.NewSimpleClientset()
	mapper := NewRegionMapper(client)
	if mapper == nil {
		t.Fatal("NewRegionMapper returned nil")
	}

	// Verify default values
	if mapper.config.DefaultElectricityMapsZone != "US-CAL-CISO" {
		t.Errorf("Expected default electricity maps zone to be US-CAL-CISO, got %s", mapper.config.DefaultElectricityMapsZone)
	}

	if mapper.config.DefaultTimeZone != "America/Los_Angeles" {
		t.Errorf("Expected default time zone to be America/Los_Angeles, got %s", mapper.config.DefaultTimeZone)
	}

	if mapper.config.DefaultPUE != 1.2 {
		t.Errorf("Expected default PUE to be 1.2, got %f", mapper.config.DefaultPUE)
	}

	// Verify maps are initialized
	if len(mapper.awsRegionMap) == 0 {
		t.Error("AWS region map is empty")
	}

	if len(mapper.gcpRegionMap) == 0 {
		t.Error("GCP region map is empty")
	}

	if len(mapper.azureRegionMap) == 0 {
		t.Error("Azure region map is empty")
	}
}

func TestNewRegionMapperWithConfig(t *testing.T) {
	// Test with nil config
	client := fake.NewSimpleClientset()
	mapper := NewRegionMapperWithConfig(client, nil)
	if mapper == nil {
		t.Fatal("NewRegionMapperWithConfig(nil) returned nil")
	}

	// Test with a custom config
	customConfig := &Config{
		DefaultElectricityMapsZone: "US-NYC",
		DefaultTimeZone:            "America/New_York",
		DefaultPUE:                 1.35,
		RegionOverrides: []RegionOverride{
			{
				Provider:            "aws",
				Region:              "us-west-1",
				ElectricityMapsZone: "US-WEST-CUSTOM",
				TimeZone:            "America/Los_Angeles",
				ISO:                 "CAISO",
				PUE:                 1.1,
			},
		},
	}

	mapper = NewRegionMapperWithConfig(client, customConfig)
	if mapper == nil {
		t.Fatal("NewRegionMapperWithConfig(customConfig) returned nil")
	}

	// Verify custom default values
	if mapper.config.DefaultElectricityMapsZone != "US-NYC" {
		t.Errorf("Expected default electricity maps zone to be US-NYC, got %s", mapper.config.DefaultElectricityMapsZone)
	}

	if mapper.config.DefaultTimeZone != "America/New_York" {
		t.Errorf("Expected default time zone to be America/New_York, got %s", mapper.config.DefaultTimeZone)
	}

	if mapper.config.DefaultPUE != 1.35 {
		t.Errorf("Expected default PUE to be 1.35, got %f", mapper.config.DefaultPUE)
	}

	// Verify region override was applied
	info, ok := mapper.GetRegionInfo("aws", "us-west-1")
	if !ok {
		t.Fatal("Failed to get region info for overridden region")
	}

	if info.ElectricityMapsZone != "US-WEST-CUSTOM" {
		t.Errorf("Expected custom electricity maps zone, got %s", info.ElectricityMapsZone)
	}

	if info.TimeZone != "America/Los_Angeles" {
		t.Errorf("Expected custom time zone, got %s", info.TimeZone)
	}

	if info.ISO != "CAISO" {
		t.Errorf("Expected custom ISO, got %s", info.ISO)
	}

	if info.DefaultPUE != 1.1 {
		t.Errorf("Expected custom PUE, got %f", info.DefaultPUE)
	}
}

func TestGetRegionInfo(t *testing.T) {
	client := fake.NewSimpleClientset()
	mapper := NewRegionMapper(client)

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
		// Test case-insensitive provider handling
		{"AWS", "us-east-1", true, 1.2},
		{"Gcp", "us-west1", true, 1.09},
		{"AZURE", "eastus", true, 1.25},
		// Test prefix matching
		{"aws", "us-east-1-zone-a", true, 1.2}, // Should match us-east-1
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

func TestGetRegionInfoForNode(t *testing.T) {
	client := fake.NewSimpleClientset()
	mapper := NewRegionMapper(client)

	testCases := []struct {
		name     string
		node     *v1.Node
		expectOk bool
	}{
		{
			name: "aws node with provider ID",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "aws-node",
				},
				Spec: v1.NodeSpec{
					ProviderID: "aws://us-east-1/i-12345",
				},
			},
			expectOk: true,
		},
		{
			name: "aws node with topology labels",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "aws-node-topology",
					Labels: map[string]string{
						"topology.kubernetes.io/region":    "us-west-2",
						"node.kubernetes.io/instance-type": "m5.large",
					},
				},
			},
			expectOk: true,
		},
		{
			name: "node with no provider info",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unknown-node",
				},
			},
			expectOk: false,
		},
		{
			name:     "nil node",
			node:     nil,
			expectOk: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			info, ok := mapper.GetRegionInfoForNode(tc.node)
			if ok != tc.expectOk {
				t.Errorf("GetRegionInfoForNode() returned ok=%v, expected %v", ok, tc.expectOk)
			}

			if tc.expectOk && info == nil {
				t.Error("GetRegionInfoForNode() returned nil info when ok=true")
			}
		})
	}
}

func TestGetElectricityMapsZone(t *testing.T) {
	client := fake.NewSimpleClientset()
	mapper := NewRegionMapper(client)

	// First get the actual zone values from the initialized mapper
	usEast1Zone, _ := mapper.GetElectricityMapsZone("aws", "us-east-1")
	usWest1Zone, _ := mapper.GetElectricityMapsZone("gcp", "us-west1")
	eastusZone, _ := mapper.GetElectricityMapsZone("azure", "eastus")

	testCases := []struct {
		provider   string
		region     string
		expectOk   bool
		expectZone string
	}{
		{"aws", "us-east-1", true, usEast1Zone},
		{"gcp", "us-west1", true, usWest1Zone},
		{"azure", "eastus", true, eastusZone},
		{"unknown", "region", false, ""},
		{"aws", "unknown-region", false, ""},
		// Test case-insensitive provider handling
		{"AWS", "us-east-1", true, usEast1Zone},
		// Test prefix matching
		{"aws", "us-east-1-zone-a", true, usEast1Zone},
	}

	for _, tc := range testCases {
		zone, ok := mapper.GetElectricityMapsZone(tc.provider, tc.region)
		if ok != tc.expectOk {
			t.Errorf("GetElectricityMapsZone(%s, %s) returned ok=%v, expected %v", tc.provider, tc.region, ok, tc.expectOk)
		}

		if tc.expectOk && zone != tc.expectZone {
			t.Errorf("GetElectricityMapsZone(%s, %s) returned zone=%s, expected %s", tc.provider, tc.region, zone, tc.expectZone)
		}
	}
}

func TestGetElectricityMapsZoneForNode(t *testing.T) {
	client := fake.NewSimpleClientset()
	mapper := NewRegionMapper(client)

	// Get the actual expected zone for us-east-1
	usEast1Zone, _ := mapper.GetElectricityMapsZone("aws", "us-east-1")

	testCases := []struct {
		name       string
		node       *v1.Node
		expectOk   bool
		expectZone string
	}{
		{
			name: "aws node with provider ID",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "aws-node",
				},
				Spec: v1.NodeSpec{
					ProviderID: "aws://us-east-1/i-12345",
				},
			},
			expectOk:   true,
			expectZone: usEast1Zone,
		},
		{
			name: "node with no provider info",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unknown-node",
				},
			},
			expectOk:   false,
			expectZone: "",
		},
		{
			name:       "nil node",
			node:       nil,
			expectOk:   false,
			expectZone: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			zone, ok := mapper.GetElectricityMapsZoneForNode(tc.node)
			if ok != tc.expectOk {
				t.Errorf("GetElectricityMapsZoneForNode() returned ok=%v, expected %v", ok, tc.expectOk)
			}

			if tc.expectOk && zone != tc.expectZone {
				t.Errorf("GetElectricityMapsZoneForNode() returned zone=%s, expected %s", zone, tc.expectZone)
			}
		})
	}

	// Test with default when region not found
	defaultZone := "DEFAULT-ZONE"
	defaultClient := fake.NewSimpleClientset()
	defaultMapper := NewRegionMapperWithConfig(defaultClient, &Config{
		DefaultElectricityMapsZone: defaultZone,
	})

	// Use a valid provider but unknown region
	unknownNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "aws-unknown-region",
			Labels: map[string]string{
				"node.kubernetes.io/instance-type": "m5.large",            // This will trigger AWS provider detection
				"topology.kubernetes.io/region":    "non-existent-region", // Use a region that doesn't exist
			},
		},
	}

	zone, ok := defaultMapper.GetElectricityMapsZoneForNode(unknownNode)
	if !ok {
		t.Error("Expected ok=true when default zone is available")
	}
	if zone != defaultZone {
		t.Errorf("Expected default zone %s, got %s", defaultZone, zone)
	}
}

func TestGetTimeZone(t *testing.T) {
	// Create a mapper without defaults to test direct mappings
	mapperWithoutDefaults := &RegionMapper{
		config: &Config{
			DefaultTimeZone: "", // Explicitly empty to not use defaults
		},
		awsRegionMap:           make(map[string]RegionInfo),
		gcpRegionMap:           make(map[string]RegionInfo),
		azureRegionMap:         make(map[string]RegionInfo),
		electricityMapsZoneMap: make(map[string]map[string]string),
	}

	// Add known test values
	mapperWithoutDefaults.awsRegionMap["us-east-1"] = RegionInfo{
		TimeZone: "America/New_York",
	}
	mapperWithoutDefaults.gcpRegionMap["us-west1"] = RegionInfo{
		TimeZone: "America/Los_Angeles",
	}
	mapperWithoutDefaults.azureRegionMap["eastus"] = RegionInfo{
		TimeZone: "America/New_York",
	}

	testCases := []struct {
		provider string
		region   string
		expectOk bool
		expectTZ string
	}{
		{"aws", "us-east-1", true, "America/New_York"},
		{"gcp", "us-west1", true, "America/Los_Angeles"},
		{"azure", "eastus", true, "America/New_York"},
		{"unknown", "region", false, ""},
		{"aws", "unknown-region", false, ""},
	}

	for _, tc := range testCases {
		tz, ok := mapperWithoutDefaults.GetTimeZone(tc.provider, tc.region)
		if ok != tc.expectOk {
			t.Errorf("GetTimeZone(%s, %s) returned ok=%v, expected %v", tc.provider, tc.region, ok, tc.expectOk)
		}

		if tc.expectOk && tz != tc.expectTZ {
			t.Errorf("GetTimeZone(%s, %s) returned timezone=%s, expected %s", tc.provider, tc.region, tz, tc.expectTZ)
		}
	}

	// Test with default when region not found
	defaultTZ := "UTC"
	defaultClient := fake.NewSimpleClientset()
	defaultMapper := NewRegionMapperWithConfig(defaultClient, &Config{
		DefaultTimeZone: defaultTZ,
	})

	tz, ok := defaultMapper.GetTimeZone("unknown", "region")
	if !ok {
		t.Error("Expected ok=true when default timezone is available")
	}
	if tz != defaultTZ {
		t.Errorf("Expected default timezone %s, got %s", defaultTZ, tz)
	}
}

func TestGetTimeZoneForNode(t *testing.T) {
	client := fake.NewSimpleClientset()
	mapper := NewRegionMapper(client)

	testCases := []struct {
		name     string
		node     *v1.Node
		expectOk bool
	}{
		{
			name: "aws node with provider ID",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "aws-node",
				},
				Spec: v1.NodeSpec{
					ProviderID: "aws://us-east-1/i-12345",
				},
			},
			expectOk: true,
		},
		{
			name: "node with no provider info",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unknown-node",
				},
			},
			expectOk: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := mapper.GetTimeZoneForNode(tc.node)
			if ok != tc.expectOk {
				t.Errorf("GetTimeZoneForNode() returned ok=%v, expected %v", ok, tc.expectOk)
			}
		})
	}
}

func TestGetISO(t *testing.T) {
	client := fake.NewSimpleClientset()
	mapper := NewRegionMapper(client)

	// Get actual ISO values from the mapper
	usEast1ISO, _ := mapper.GetISO("aws", "us-east-1")
	usWest1ISO, _ := mapper.GetISO("gcp", "us-west1")

	testCases := []struct {
		provider  string
		region    string
		expectOk  bool
		expectISO string
	}{
		{"aws", "us-east-1", true, usEast1ISO},
		{"gcp", "us-west1", true, usWest1ISO},
		{"unknown", "region", false, ""},
	}

	for _, tc := range testCases {
		iso, ok := mapper.GetISO(tc.provider, tc.region)
		if ok != tc.expectOk {
			t.Errorf("GetISO(%s, %s) returned ok=%v, expected %v", tc.provider, tc.region, ok, tc.expectOk)
		}

		if tc.expectOk && iso != tc.expectISO {
			t.Errorf("GetISO(%s, %s) returned ISO=%s, expected %s", tc.provider, tc.region, iso, tc.expectISO)
		}
	}
}

func TestGetISOForNode(t *testing.T) {
	client := fake.NewSimpleClientset()
	mapper := NewRegionMapper(client)

	testCases := []struct {
		name     string
		node     *v1.Node
		expectOk bool
	}{
		{
			name: "aws node with provider ID",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "aws-node",
				},
				Spec: v1.NodeSpec{
					ProviderID: "aws://us-east-1/i-12345",
				},
			},
			expectOk: true,
		},
		{
			name: "node with no provider info",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unknown-node",
				},
			},
			expectOk: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := mapper.GetISOForNode(tc.node)
			if ok != tc.expectOk {
				t.Errorf("GetISOForNode() returned ok=%v, expected %v", ok, tc.expectOk)
			}
		})
	}
}

func TestGetPUE(t *testing.T) {
	// Create a mapper without defaults to test direct mappings
	mapperWithoutDefaults := &RegionMapper{
		config: &Config{
			DefaultPUE: 0, // Explicitly zero to not use defaults
		},
		awsRegionMap:           make(map[string]RegionInfo),
		gcpRegionMap:           make(map[string]RegionInfo),
		azureRegionMap:         make(map[string]RegionInfo),
		electricityMapsZoneMap: make(map[string]map[string]string),
	}

	// Add known test values
	mapperWithoutDefaults.awsRegionMap["us-east-1"] = RegionInfo{
		DefaultPUE: 1.2,
	}
	mapperWithoutDefaults.gcpRegionMap["us-west1"] = RegionInfo{
		DefaultPUE: 1.09,
	}

	testCases := []struct {
		provider  string
		region    string
		expectOk  bool
		expectPUE float64
	}{
		{"aws", "us-east-1", true, 1.2},
		{"gcp", "us-west1", true, 1.09},
		{"unknown", "region", false, 0},
		{"aws", "unknown-region", false, 0},
	}

	for _, tc := range testCases {
		pue, ok := mapperWithoutDefaults.GetPUE(tc.provider, tc.region)
		if ok != tc.expectOk {
			t.Errorf("GetPUE(%s, %s) returned ok=%v, expected %v", tc.provider, tc.region, ok, tc.expectOk)
		}

		if tc.expectOk && pue != tc.expectPUE {
			t.Errorf("GetPUE(%s, %s) returned PUE=%f, expected %f", tc.provider, tc.region, pue, tc.expectPUE)
		}
	}

	// Test with default when region not found
	defaultPUE := 1.5
	defaultClient := fake.NewSimpleClientset()
	defaultMapper := NewRegionMapperWithConfig(defaultClient, &Config{
		DefaultPUE: defaultPUE,
	})

	pue, ok := defaultMapper.GetPUE("unknown", "region")
	if !ok {
		t.Error("Expected ok=true when default PUE is available")
	}
	if pue != defaultPUE {
		t.Errorf("Expected default PUE %f, got %f", defaultPUE, pue)
	}
}

func TestGetPUEForNode(t *testing.T) {
	client := fake.NewSimpleClientset()
	mapper := NewRegionMapper(client)

	testCases := []struct {
		name     string
		node     *v1.Node
		expectOk bool
	}{
		{
			name: "aws node with provider ID",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "aws-node",
				},
				Spec: v1.NodeSpec{
					ProviderID: "aws://us-east-1/i-12345",
				},
			},
			expectOk: true,
		},
		{
			name: "node with no provider info",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unknown-node",
				},
			},
			expectOk: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := mapper.GetPUEForNode(tc.node)
			if ok != tc.expectOk {
				t.Errorf("GetPUEForNode() returned ok=%v, expected %v", ok, tc.expectOk)
			}
		})
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
		{"unknown://provider", map[string]string{}, ""},
		{"", nil, ""},
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

	// Test with nil node
	provider := DetectCloudProvider(nil)
	if provider != "" {
		t.Errorf("DetectCloudProvider(nil) returned %q, expected empty string", provider)
	}
}

func TestDetectCloudProviderAndRegion(t *testing.T) {
	testCases := []struct {
		name           string
		node           *v1.Node
		expectProvider string
		expectRegion   string
		expectOk       bool
	}{
		{
			name: "AWS node with providerID",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "aws://us-east-1/i-12345",
				},
			},
			expectProvider: "aws",
			expectRegion:   "us-east-1",
			expectOk:       true,
		},
		{
			name: "AWS node with topology label",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"topology.kubernetes.io/region":    "us-west-2",
						"node.kubernetes.io/instance-type": "m5.large",
					},
				},
			},
			expectProvider: "aws",
			expectRegion:   "us-west-2",
			expectOk:       true,
		},
		{
			name: "GCP node with providerID",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "gce://project-id/us-central1-a/vm-1",
				},
			},
			expectProvider: "gcp",
			expectRegion:   "us-central1",
			expectOk:       true,
		},
		{
			name: "GCP node with topology and zone",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"topology.kubernetes.io/zone":   "us-west1-a",
						"cloud.google.com/gke-nodepool": "pool-1",
					},
				},
			},
			expectProvider: "gcp",
			expectRegion:   "us-west1",
			expectOk:       true,
		},
		{
			name: "Node with provider but no region info",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "m5.large",
					},
				},
			},
			expectProvider: "aws",
			expectRegion:   "",
			expectOk:       false,
		},
		{
			name: "Node with no provider info",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"unrelated": "label",
					},
				},
			},
			expectProvider: "",
			expectRegion:   "",
			expectOk:       false,
		},
		{
			name:           "Nil node",
			node:           nil,
			expectProvider: "",
			expectRegion:   "",
			expectOk:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider, region, ok := DetectCloudProviderAndRegion(tc.node)

			if provider != tc.expectProvider {
				t.Errorf("Expected provider %q, got %q", tc.expectProvider, provider)
			}

			if region != tc.expectRegion {
				t.Errorf("Expected region %q, got %q", tc.expectRegion, region)
			}

			if ok != tc.expectOk {
				t.Errorf("Expected ok=%v, got %v", tc.expectOk, ok)
			}
		})
	}
}
