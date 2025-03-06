package tou

import (
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

// Scheduler handles time-of-use electricity pricing schedules
type Scheduler struct {
	config config.PricingConfig
}

// New creates a new TOU pricing scheduler
func New(config config.PricingConfig) *Scheduler {
	return &Scheduler{
		config: config,
	}
}

// GetCurrentRate returns the current electricity rate based on configured schedules
func (s *Scheduler) GetCurrentRate(now time.Time) float64 {
	weekday := fmt.Sprintf("%d", now.Weekday())
	currentTime := now.Format("15:04")

	for _, schedule := range s.config.Schedules {
		// Check if current day is in schedule
		if !containsDay(schedule.DayOfWeek, weekday) {
			continue
		}

		// Check if current time is within schedule
		if currentTime >= schedule.StartTime && currentTime <= schedule.EndTime {
			return schedule.PeakRate
		}
	}

	// If no peak schedule matches, return off-peak rate from first schedule
	// All schedules should have same off-peak rate (validated in config)
	if len(s.config.Schedules) > 0 {
		return s.config.Schedules[0].OffPeakRate
	}

	return 0 // No schedules configured
}

// CheckPriceConstraints checks if current electricity rate exceeds pod's threshold
func (s *Scheduler) CheckPriceConstraints(pod *v1.Pod, now time.Time) *framework.Status {
	rate := s.GetCurrentRate(now)

	// Get threshold from pod annotation or use off-peak rate as threshold
	var threshold float64
	if val, ok := pod.Annotations[common.AnnotationPriceThreshold]; ok {
		klog.V(2).InfoS("Found price threshold annotation",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"value", val)
		if t, err := strconv.ParseFloat(val, 64); err == nil {
			threshold = t
			klog.V(2).InfoS("Using price threshold from annotation",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"threshold", threshold)
		} else {
			klog.ErrorS(err, "Invalid price threshold annotation",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"value", val)
			return framework.NewStatus(framework.Error, "invalid electricity price threshold annotation")
		}
	} else if len(s.config.Schedules) > 0 {
		// Use off-peak rate as default threshold
		threshold = s.config.Schedules[0].OffPeakRate
		klog.V(2).InfoS("Using off-peak rate as price threshold",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"threshold", threshold)
	} else {
		return framework.NewStatus(framework.Error, "no pricing schedules configured")
	}

	if rate > threshold {
		return framework.NewStatus(
			framework.Unschedulable,
			fmt.Sprintf("Current electricity rate ($%.3f/kWh) exceeds threshold ($%.3f/kWh)",
				rate,
				threshold),
		)
	}

	return framework.NewStatus(framework.Success, "")
}

// containsDay checks if a day is included in a day string (e.g. "1,2,3" contains "2")
func containsDay(days string, day string) bool {
	for _, d := range days {
		if string(d) == day {
			return true
		}
	}
	return false
}
