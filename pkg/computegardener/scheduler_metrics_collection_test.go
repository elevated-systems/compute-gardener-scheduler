package computegardener

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetAssignedGPUUUIDs(t *testing.T) {
	tests := []struct {
		name             string
		envValue         string
		expectedResult   []string
		expectNil        bool
		expectEmptySlice bool
	}{
		{
			name:             "no environment variable",
			envValue:         "",
			expectedResult:   nil,
			expectNil:        true,
			expectEmptySlice: false,
		},
		{
			name:             "empty value",
			envValue:         "",
			expectedResult:   []string{},
			expectNil:        false,
			expectEmptySlice: true,
		},
		{
			name:             "none",
			envValue:         "none",
			expectedResult:   []string{},
			expectNil:        false,
			expectEmptySlice: true,
		},
		{
			name:             "all",
			envValue:         "all",
			expectedResult:   []string{"all"},
			expectNil:        false,
			expectEmptySlice: false,
		},
		{
			name:             "single UUID",
			envValue:         "GPU-e0b9c0ec-3a68-cd5f-c41f-5c9466dcd83e",
			expectedResult:   []string{"GPU-e0b9c0ec-3a68-cd5f-c41f-5c9466dcd83e"},
			expectNil:        false,
			expectEmptySlice: false,
		},
		{
			name:             "multiple UUIDs",
			envValue:         "GPU-e0b9c0ec-3a68-cd5f-c41f-5c9466dcd83e,GPU-a1b2c3d4-5e6f-7g8h-9i0j-1k2l3m4n5o6p",
			expectedResult:   []string{"GPU-e0b9c0ec-3a68-cd5f-c41f-5c9466dcd83e", "GPU-a1b2c3d4-5e6f-7g8h-9i0j-1k2l3m4n5o6p"},
			expectNil:        false,
			expectEmptySlice: false,
		},
		{
			name:             "numeric indices",
			envValue:         "0,1",
			expectedResult:   []string{"indices:0,1"},
			expectNil:        false,
			expectEmptySlice: false,
		},
		{
			name:             "single numeric index",
			envValue:         "0",
			expectedResult:   []string{"indices:0"},
			expectNil:        false,
			expectEmptySlice: false,
		},
		{
			name:             "mixed numeric and UUID", // Not expected in real-world, but ensure basic handling if ever encountered.
			envValue:         "0,GPU-e0b9c0ec-3a68-cd5f-c41f-5c9466dcd83e",
			expectedResult:   []string{"0", "GPU-e0b9c0ec-3a68-cd5f-c41f-5c9466dcd83e"},
			expectNil:        false,
			expectEmptySlice: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Env:  []v1.EnvVar{},
						},
					},
				},
			}

			// For the "no environment variable" test, don't add the env var
			if tt.name != "no environment variable" {
				pod.Spec.Containers[0].Env = []v1.EnvVar{
					{
						Name:  "NVIDIA_VISIBLE_DEVICES",
						Value: tt.envValue,
					},
				}
			}

			result := getAssignedGPUUUIDs(pod)

			// Check nil specifically
			if tt.expectNil && result != nil {
				t.Errorf("Expected nil result, got %v", result)
				return
			}

			// Check empty slice specifically
			if tt.expectEmptySlice && (result == nil || len(result) != 0) {
				t.Errorf("Expected empty slice, got %v", result)
				return
			}

			// For non-nil and non-empty cases, check the expected result
			if !tt.expectNil && !tt.expectEmptySlice {
				if len(result) != len(tt.expectedResult) {
					t.Errorf("Expected result length %d, got %d", len(tt.expectedResult), len(result))
					return
				}

				for i, v := range result {
					if v != tt.expectedResult[i] {
						t.Errorf("Expected result[%d]=%s, got %s", i, tt.expectedResult[i], v)
					}
				}
			}
		})
	}
}
