package tou

import (
	"fmt"
	"strconv"
	"strings"
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

// IsCurrentlyPeakTime checks if the current time is within a peak time window
func (s *Scheduler) IsCurrentlyPeakTime(now time.Time) bool {
	weekday := fmt.Sprintf("%d", now.Weekday())
	currentTime := now.Format("15:04")
	
	klog.V(2).InfoS("Checking if current time is within peak window",
		"currentWeekday", weekday, 
		"currentTime", currentTime,
		"numSchedules", len(s.config.Schedules))

	for idx, schedule := range s.config.Schedules {
		// Check if current day is in schedule
		isDayMatched := containsDay(schedule.DayOfWeek, weekday)
		klog.V(2).InfoS("Checking schedule",
			"scheduleIndex", idx,
			"dayOfWeek", schedule.DayOfWeek,
			"startTime", schedule.StartTime,
			"endTime", schedule.EndTime,
			"isDayMatched", isDayMatched)
			
		if !isDayMatched {
			continue
		}

		// Check if current time is within schedule
		isTimeInRange := currentTime >= schedule.StartTime && currentTime <= schedule.EndTime
		klog.V(2).InfoS("Checking time range",
			"scheduleIndex", idx,
			"currentTime", currentTime,
			"startTime", schedule.StartTime,
			"endTime", schedule.EndTime,
			"isInRange", isTimeInRange)
			
		if isTimeInRange {
			klog.V(2).InfoS("Current time is within peak period", 
				"weekday", weekday,
				"currentTime", currentTime,
				"schedule", fmt.Sprintf("%s from %s to %s", 
					describeDays(schedule.DayOfWeek), 
					schedule.StartTime, 
					schedule.EndTime))
			return true
		}
	}

	klog.V(2).InfoS("Current time is NOT within any peak window", 
		"weekday", weekday,
		"currentTime", currentTime)
	return false // Not in any peak time window
}

// GetCurrentRate returns the current electricity rate based on configured schedules
// Used mainly for metrics and reporting, not for scheduling decisions
func (s *Scheduler) GetCurrentRate(now time.Time) float64 {
	// If we don't have rates configured, return nominal values for peak/off-peak
	if len(s.config.Schedules) == 0 {
		return 0 // No schedules configured
	}

	// If first schedule doesn't have rates configured, return 1.0 for peak, 0.5 for off-peak
	// as nominal values for metric recording purposes only
	schedule := s.config.Schedules[0]
	peakRate := 1.0
	offPeakRate := 0.5

	// If rates are configured, use the actual rates
	if schedule.PeakRate > 0 && schedule.OffPeakRate > 0 {
		peakRate = schedule.PeakRate
		offPeakRate = schedule.OffPeakRate
	}
	
	if s.IsCurrentlyPeakTime(now) {
		return peakRate
	}
	
	return offPeakRate
}

// CheckPriceConstraints checks if scheduling should be allowed based on time-of-use schedule
func (s *Scheduler) CheckPriceConstraints(pod *v1.Pod, now time.Time) *framework.Status {
	// Default behavior: Don't schedule during peak times unless annotated to allow it
	allowPeakScheduling := false
	
	// Check if pod has annotation to explicitly allow peak scheduling
	if val, ok := pod.Annotations[common.AnnotationPriceThreshold]; ok {
		klog.V(2).InfoS("Found price threshold annotation",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"value", val)
			
		// If the pod has a threshold annotation, it's essentially setting a custom allowance value
		// A high threshold effectively allows scheduling during peak times
		if t, err := strconv.ParseFloat(val, 64); err == nil {
			// If rates are configured, check against actual peak rate
			// Otherwise, just check if the threshold is high (>0.9 of nominal 1.0 peak)
			if len(s.config.Schedules) > 0 && s.config.Schedules[0].PeakRate > 0 {
				allowPeakScheduling = t >= s.config.Schedules[0].PeakRate
			} else {
				allowPeakScheduling = t >= 0.9 // Arbitrary high threshold compared to nominal value
			}
			
			klog.V(2).InfoS("Evaluated price threshold annotation",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"threshold", t,
				"allowPeakScheduling", allowPeakScheduling)
		} else {
			klog.ErrorS(err, "Invalid price threshold annotation",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"value", val)
			return framework.NewStatus(framework.Error, "invalid electricity price threshold annotation")
		}
	}
	
	// If we're in peak time and the pod isn't allowed to schedule during peak, block scheduling
	if s.IsCurrentlyPeakTime(now) && !allowPeakScheduling {
		// For human-readable message, get the current schedule period for context
		scheduleInfo := "current peak period"
		for _, schedule := range s.config.Schedules {
			weekday := fmt.Sprintf("%d", now.Weekday())
			if containsDay(schedule.DayOfWeek, weekday) {
				currentTime := now.Format("15:04")
				if currentTime >= schedule.StartTime && currentTime <= schedule.EndTime {
					days := describeDays(schedule.DayOfWeek)
					scheduleInfo = fmt.Sprintf("%s from %s to %s", days, schedule.StartTime, schedule.EndTime)
					break
				}
			}
		}

		// Record this in metrics if rates are available
		if len(s.config.Schedules) > 0 {
			schedule := s.config.Schedules[0]
			if schedule.PeakRate > 0 && schedule.OffPeakRate > 0 {
				savingsEstimate := schedule.PeakRate - schedule.OffPeakRate
				klog.V(2).InfoS("Estimated savings for delaying pod",
					"pod", klog.KObj(pod),
					"savingsPerKWh", savingsEstimate)
			}
		}

		return framework.NewStatus(
			framework.Unschedulable,
			fmt.Sprintf("Current time is during peak electricity hours (%s)", scheduleInfo),
		)
	}

	return framework.NewStatus(framework.Success, "")
}

// containsDay checks if a day is included in a day string (e.g. "1,2,3" contains "2")
// Also handles day ranges like "1-5" contains "3"
func containsDay(days string, day string) bool {
	dayNum, err := strconv.Atoi(day)
	if err != nil {
		return false
	}

	parts := strings.Split(days, ",")
	for _, part := range parts {
		if strings.Contains(part, "-") {
			// Handle range format (e.g., "1-5")
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				continue
			}
			
			start, err := strconv.Atoi(rangeParts[0])
			if err != nil {
				continue
			}
			
			end, err := strconv.Atoi(rangeParts[1])
			if err != nil {
				continue
			}
			
			if dayNum >= start && dayNum <= end {
				return true
			}
		} else if part == day {
			return true
		}
	}
	
	return false
}

// describeDays converts day numbers to human-readable form
func describeDays(daySpec string) string {
	parts := strings.Split(daySpec, ",")
	if len(parts) == 0 {
		return "unknown days"
	}
	
	// Handle common patterns
	if daySpec == "1-5" {
		return "weekdays"
	} else if daySpec == "0,6" {
		return "weekends"
	} else if daySpec == "0-6" {
		return "all days"
	}
	
	// Map day numbers to names
	dayNames := map[string]string{
		"0": "Sunday",
		"1": "Monday",
		"2": "Tuesday", 
		"3": "Wednesday",
		"4": "Thursday",
		"5": "Friday",
		"6": "Saturday",
	}
	
	var result []string
	for _, part := range parts {
		if strings.Contains(part, "-") {
			// Handle range
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) == 2 {
				start, startOk := dayNames[rangeParts[0]]
				end, endOk := dayNames[rangeParts[1]]
				if startOk && endOk {
					result = append(result, fmt.Sprintf("%s-%s", start, end))
				} else {
					result = append(result, part)
				}
			} else {
				result = append(result, part)
			}
		} else {
			// Handle single day
			if name, ok := dayNames[part]; ok {
				result = append(result, name)
			} else {
				result = append(result, part)
			}
		}
	}
	
	return strings.Join(result, ", ")
}
