package metrics

import (
	"math"
	"testing"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/types"
)

// Helper function to create test metric records
func createTestMetricRecords(count int, startTime time.Time, interval time.Duration) []types.PodMetricsRecord {
	records := make([]types.PodMetricsRecord, count)
	for i := 0; i < count; i++ {
		// Generate records with time interval and a sine wave pattern for CPU usage
		records[i] = types.PodMetricsRecord{
			Timestamp:     startTime.Add(interval * time.Duration(i)),
			CPU:           math.Sin(float64(i)*0.5) + 2.0,     // CPU varies between 1.0 and 3.0
			Memory:        float64(1024 * 1024 * (50 + i%20)), // Memory varies between 50-70 MB
			PowerEstimate: 50 + 20*math.Sin(float64(i)*0.5),   // Power varies between 30-70W
		}
	}
	return records
}

func TestLTTBDownsampling(t *testing.T) {
	startTime := time.Now().Add(-time.Hour) // Start 1 hour ago
	interval := time.Minute                 // 1 record per minute

	testCases := []struct {
		name               string
		recordCount        int
		targetCount        int
		expectedResultSize int
	}{
		{
			name:               "TargetLargerThanSource",
			recordCount:        10,
			targetCount:        20,
			expectedResultSize: 10, // Should return all records
		},
		{
			name:               "TargetEqualsSource",
			recordCount:        10,
			targetCount:        10,
			expectedResultSize: 10, // Should return all records
		},
		{
			name:               "TargetOne",
			recordCount:        10,
			targetCount:        1,
			expectedResultSize: 1, // Should return only first point
		},
		{
			name:               "TargetTwo",
			recordCount:        10,
			targetCount:        2,
			expectedResultSize: 2, // Should return first and last points
		},
		{
			name:               "NormalDownsample",
			recordCount:        100,
			targetCount:        10,
			expectedResultSize: 10, // Should return exactly targetCount points
		},
	}

	downsample := &LTTBDownsampling{}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			records := createTestMetricRecords(tc.recordCount, startTime, interval)
			result := downsample.Downsample(records, tc.targetCount)

			// Verify the result has the expected number of points
			if len(result) != tc.expectedResultSize {
				t.Errorf("Expected %d records, got %d", tc.expectedResultSize, len(result))
			}

			// For normal downsampling, first and last points should be preserved
			if tc.targetCount >= 3 && tc.targetCount < tc.recordCount {
				if result[0].Timestamp != records[0].Timestamp {
					t.Errorf("First point not preserved in downsampling")
				}
				if result[len(result)-1].Timestamp != records[len(records)-1].Timestamp {
					t.Errorf("Last point not preserved in downsampling")
				}
			}
		})
	}
}

func TestSimpleTimeBasedDownsampling(t *testing.T) {
	startTime := time.Now().Add(-time.Hour) // Start 1 hour ago
	interval := time.Minute                 // 1 record per minute

	testCases := []struct {
		name               string
		recordCount        int
		targetCount        int
		expectedResultSize int
	}{
		{
			name:               "TargetLargerThanSource",
			recordCount:        10,
			targetCount:        20,
			expectedResultSize: 10, // Should return all records
		},
		{
			name:               "TargetEqualsSource",
			recordCount:        10,
			targetCount:        10,
			expectedResultSize: 10, // Should return all records
		},
		{
			name:               "NormalDownsample",
			recordCount:        100,
			targetCount:        20,
			expectedResultSize: 19, // Slight deviation due to division of points between recent and older data
		},
		{
			name:               "HighlyRestrictiveTarget",
			recordCount:        100,
			targetCount:        5,
			expectedResultSize: 4, // Points allocated between recent and older data with rounding
		},
	}

	downsample := &SimpleTimeBasedDownsampling{}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			records := createTestMetricRecords(tc.recordCount, startTime, interval)
			result := downsample.Downsample(records, tc.targetCount)

			// Verify the result has the expected number of points
			if len(result) != tc.expectedResultSize {
				t.Errorf("Expected %d records, got %d", tc.expectedResultSize, len(result))
			}

			// First point should be preserved
			if tc.recordCount > 0 && tc.targetCount > 0 {
				if result[0].Timestamp != records[0].Timestamp {
					t.Errorf("First point not preserved in downsampling")
				}
			}

			// Recent points should be well represented
			if tc.targetCount < tc.recordCount && tc.targetCount > 1 {
				// Check that the last point is preserved
				if result[len(result)-1].Timestamp != records[len(records)-1].Timestamp {
					t.Errorf("Last point not preserved in downsampling")
				}
			}
		})
	}
}

func TestMinMaxDownsampling(t *testing.T) {
	startTime := time.Now().Add(-time.Hour) // Start 1 hour ago
	interval := time.Minute                 // 1 record per minute

	testCases := []struct {
		name               string
		recordCount        int
		targetCount        int
		expectedResultSize int
	}{
		{
			name:               "TargetLargerThanSource",
			recordCount:        10,
			targetCount:        20,
			expectedResultSize: 10, // Should return all records
		},
		{
			name:               "TargetEqualsSource",
			recordCount:        10,
			targetCount:        10,
			expectedResultSize: 10, // Should return all records
		},
		{
			name:               "TargetOne",
			recordCount:        10,
			targetCount:        1,
			expectedResultSize: 2, // Algorithm always includes first and last points
		},
		{
			name:               "TargetTwo",
			recordCount:        10,
			targetCount:        2,
			expectedResultSize: 2, // Should return first and last points
		},
		{
			name:               "NormalDownsample",
			recordCount:        100,
			targetCount:        20,
			expectedResultSize: 20, // Should return exactly targetCount points
		},
	}

	downsample := &MinMaxDownsampling{}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			records := createTestMetricRecords(tc.recordCount, startTime, interval)
			result := downsample.Downsample(records, tc.targetCount)

			// Verify the result has the expected number of points
			// For MinMaxDownsampling, we expect a specific result size based on the algorithm's behavior
			if len(result) != tc.expectedResultSize {
				t.Errorf("Expected %d records, got %d", tc.expectedResultSize, len(result))
			}

			// First point should be preserved
			if tc.recordCount > 0 && tc.targetCount > 0 {
				if result[0].Timestamp != records[0].Timestamp {
					t.Errorf("First point not preserved in downsampling")
				}
			}

			// Last point should be preserved if we have multiple records
			if tc.recordCount > 1 && tc.targetCount > 1 {
				if result[len(result)-1].Timestamp != records[len(records)-1].Timestamp {
					t.Errorf("Last point not preserved in downsampling")
				}
			}
		})
	}
}

func TestTriangleArea(t *testing.T) {
	testCases := []struct {
		name     string
		x1, y1   float64
		x2, y2   float64
		x3, y3   float64
		expected float64
	}{
		{
			name: "RightTriangle",
			x1:   0, y1: 0,
			x2: 0, y2: 3,
			x3: 4, y3: 0,
			expected: 6, // Area of right triangle with base 4 and height 3
		},
		{
			name: "EquilateralTriangle",
			x1:   0, y1: 0,
			x2: 1, y2: 1.732, // sqrt(3)
			x3: 2, y3: 0,
			expected: 1.732, // Area of equilateral triangle with side length 2
		},
		{
			name: "ZeroArea",
			x1:   1, y1: 1,
			x2: 1, y2: 1,
			x3: 1, y3: 1,
			expected: 0, // All points are the same
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			area := triangleArea(tc.x1, tc.y1, tc.x2, tc.y2, tc.x3, tc.y3)

			// Use a small epsilon for floating point comparison
			epsilon := 0.001
			if math.Abs(area-tc.expected) > epsilon {
				t.Errorf("Expected area: %f, got: %f", tc.expected, area)
			}
		})
	}
}
