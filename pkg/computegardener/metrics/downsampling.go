package metrics

import (
	"math"
	"sort"
)

// LTTBDownsampling implements the Largest-Triangle-Three-Buckets algorithm
// which provides high-quality downsampling that preserves visual appearance
type LTTBDownsampling struct{}

// Downsample reduces the number of data points while preserving trend using LTTB algorithm
// LTTB selects points that maximize the triangle area, preserving visual features
func (d *LTTBDownsampling) Downsample(records []PodMetricsRecord, targetCount int) []PodMetricsRecord {
	if targetCount >= len(records) {
		return records
	}

	// Always include first and last points
	if targetCount < 3 {
		// For very small target counts, just keep first and last
		if targetCount == 2 {
			return []PodMetricsRecord{
				records[0],
				records[len(records)-1],
			}
		}
		return []PodMetricsRecord{records[0]} // targetCount == 1
	}

	// The result will contain the original first and last points plus targetCount-2 sampled points
	result := make([]PodMetricsRecord, targetCount)
	result[0] = records[0]                    // First point
	result[targetCount-1] = records[len(records)-1] // Last point

	// Bucket size based on the original data size
	bucketSize := float64(len(records)-2) / float64(targetCount-2)

	// LTTB algorithm main loop
	lastSelectedIndex := 0
	for i := 1; i < targetCount-1; i++ {
		// Determine next bucket range
		nextBucketStart := int(math.Floor(float64(i) * bucketSize)) + 1
		nextBucketEnd := int(math.Min(
			math.Floor(float64(i+1)*bucketSize)+1,
			float64(len(records)),
		))

		// Select the point from this bucket that creates the largest triangle
		// with the last selected point and the centroid of the next bucket
		maxArea := -1.0
		maxAreaIndex := nextBucketStart

		// Calculate next bucket's centroid
		nextBucketCentroidX := 0.0
		nextBucketCentroidY := 0.0
		for j := nextBucketStart; j < nextBucketEnd; j++ {
			nextBucketCentroidX += float64(records[j].Timestamp.Unix())
			nextBucketCentroidY += records[j].CPU // Using CPU as primary metric
		}
		nextBucketCentroidX /= float64(nextBucketEnd - nextBucketStart)
		nextBucketCentroidY /= float64(nextBucketEnd - nextBucketStart)

		// Find point in current bucket that creates largest triangle
		for j := int(math.Floor(float64(i-1)*bucketSize)) + 1; j < nextBucketStart; j++ {
			// Calculate triangle area with lastSelectedIndex, j, and next bucket centroid
			area := triangleArea(
				float64(records[lastSelectedIndex].Timestamp.Unix()), records[lastSelectedIndex].CPU,
				float64(records[j].Timestamp.Unix()), records[j].CPU,
				nextBucketCentroidX, nextBucketCentroidY,
			)

			if area > maxArea {
				maxArea = area
				maxAreaIndex = j
			}
		}

		// Select the point with the largest triangle area
		result[i] = records[maxAreaIndex]
		lastSelectedIndex = maxAreaIndex
	}

	return result
}

// SimpleTimeBasedDownsampling preserves more recent points and fewer older points
type SimpleTimeBasedDownsampling struct{}

// Downsample reduces the number of data points by keeping more recent points
// and progressively fewer older points
func (d *SimpleTimeBasedDownsampling) Downsample(records []PodMetricsRecord, targetCount int) []PodMetricsRecord {
	if targetCount >= len(records) {
		return records
	}

	// Always keep first and last 20% of points as higher resolution
	totalLen := len(records)
	recentKeepCount := int(math.Ceil(float64(targetCount) * 0.6))
	recentStart := totalLen - recentKeepCount
	if recentStart < 0 {
		recentStart = 0
	}

	// Allocate remaining points to older data
	remainingPoints := targetCount - recentKeepCount
	if remainingPoints <= 0 {
		// If we can't keep any old points, just return the recent ones
		result := make([]PodMetricsRecord, 0, targetCount)
		for i := recentStart; i < totalLen; i++ {
			result = append(result, records[i])
		}
		return result
	}

	// Sample older points at larger intervals
	result := make([]PodMetricsRecord, 0, targetCount)
	
	// Always include the first point
	result = append(result, records[0])
	remainingPoints--
	
	// Sample the middle portion evenly
	if remainingPoints > 0 && recentStart > 1 {
		samplingInterval := float64(recentStart-1) / float64(remainingPoints)
		for i := 1; i < remainingPoints; i++ {
			idx := int(math.Floor(float64(i) * samplingInterval))
			if idx < recentStart {
				result = append(result, records[idx])
			}
		}
	}
	
	// Add all the recent points
	for i := recentStart; i < totalLen; i++ {
		result = append(result, records[i])
	}
	
	return result
}

// MinMaxDownsampling preserves extreme values when downsampling
type MinMaxDownsampling struct{}

// Downsample reduces the number of data points while preserving min/max values
func (d *MinMaxDownsampling) Downsample(records []PodMetricsRecord, targetCount int) []PodMetricsRecord {
	if targetCount >= len(records) {
		return records
	}

	// We always want to keep first and last points
	result := make([]PodMetricsRecord, 0, targetCount)
	result = append(result, records[0])
	
	// If targetCount is very small, we might just keep first and last
	if targetCount <= 2 {
		if len(records) > 1 {
			result = append(result, records[len(records)-1])
		}
		return result
	}
	
	// Split the remaining points into buckets
	bucketSize := float64(len(records)-2) / float64(targetCount-2)
	
	// For each bucket, keep the min and max points
	for i := 0; i < targetCount-2; i++ {
		bucketStart := int(math.Floor(float64(i)*bucketSize)) + 1
		bucketEnd := int(math.Min(math.Floor(float64(i+1)*bucketSize), float64(len(records)-1)))
		
		if bucketStart >= len(records) || bucketStart >= bucketEnd {
			continue
		}
		
		// Find min and max in this bucket
		minIdx, maxIdx := bucketStart, bucketStart
		minVal, maxVal := records[bucketStart].CPU, records[bucketStart].CPU
		
		for j := bucketStart + 1; j <= bucketEnd; j++ {
			if records[j].CPU < minVal {
				minVal = records[j].CPU
				minIdx = j
			}
			if records[j].CPU > maxVal {
				maxVal = records[j].CPU
				maxIdx = j
			}
		}
		
		// Add min and max points, but avoid duplicates
		// Sort by index for consistent ordering
		indices := []int{minIdx, maxIdx}
		sort.Ints(indices)
		
		for _, idx := range indices {
			// Check if we have room for both min and max
			if len(result) < targetCount-1 {
				result = append(result, records[idx])
			}
		}
	}
	
	// Always add the last point
	if len(records) > 1 && len(result) < targetCount {
		result = append(result, records[len(records)-1])
	}
	
	return result
}

// Utility function to calculate triangle area using cross product
func triangleArea(x1, y1, x2, y2, x3, y3 float64) float64 {
	return math.Abs((x1*(y2-y3) + x2*(y3-y1) + x3*(y1-y2)) / 2.0)
}