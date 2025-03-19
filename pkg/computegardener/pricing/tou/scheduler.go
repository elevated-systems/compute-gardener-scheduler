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

// schedulePeriod represents the result of checking a schedule against a timestamp
type schedulePeriod struct {
	schedule    *config.Schedule // The schedule that matched
	localTime   time.Time       // The time in the schedule's timezone
	isPeakTime  bool            // Whether the time is within peak hours
	tzName      string          // Name of the timezone used
	weekday     string          // Day of week as string
	currentTime string          // Current time formatted as HH:MM
}

// findActivePeriod checks if the given time falls within any schedule's peak period
// and returns details about the matching period, or nil if not in peak time
func (s *Scheduler) findActivePeriod(now time.Time) *schedulePeriod {
	klog.V(2).InfoS("Checking if current time is within any peak window",
		"utcTime", now.Format("2006-01-02 15:04:05 MST"),
		"numSchedules", len(s.config.Schedules))

	for idx, schedule := range s.config.Schedules {
		// Convert UTC time to schedule's timezone if specified, otherwise use UTC
		localTime := now
		var tzName string = "UTC"
		
		if schedule.Timezone != "" {
			if loc, err := time.LoadLocation(schedule.Timezone); err == nil {
				localTime = now.In(loc)
				tzName = schedule.Timezone
				zoneName, offset := localTime.Zone()
				isDST := localTime.IsDST()
				klog.V(2).InfoS("Using schedule timezone", 
					"scheduleName", schedule.Name,
					"timezone", schedule.Timezone,
					"utcTime", now.Format("2006-01-02 15:04:05 MST"),
					"localTime", localTime.Format("2006-01-02 15:04:05 MST"),
					"zoneName", zoneName,
					"offsetHours", offset/3600,
					"isDST", isDST)
			} else {
				klog.ErrorS(err, "Failed to load timezone, using UTC", 
					"scheduleName", schedule.Name,
					"timezone", schedule.Timezone)
			}
		}
		
		weekday := fmt.Sprintf("%d", localTime.Weekday())
		currentTime := localTime.Format("15:04")
		
		// Check if current day is in schedule
		isDayMatched := containsDay(schedule.DayOfWeek, weekday)
		klog.V(2).InfoS("Checking schedule",
			"scheduleName", schedule.Name,
			"scheduleIndex", idx,
			"dayOfWeek", schedule.DayOfWeek,
			"startTime", schedule.StartTime,
			"endTime", schedule.EndTime,
			"timezone", tzName,
			"isDayMatched", isDayMatched)
			
		if !isDayMatched {
			continue
		}

		// Check if current time is within schedule
		isTimeInRange := currentTime >= schedule.StartTime && currentTime <= schedule.EndTime
		klog.V(2).InfoS("Checking time range",
			"scheduleName", schedule.Name,
			"utcTime", now.Format("15:04"),
			"localTime", currentTime,
			"localDay", weekday,
			"startTime", schedule.StartTime,
			"endTime", schedule.EndTime,
			"timezone", tzName,
			"isInRange", isTimeInRange)
			
		if isTimeInRange {
			klog.V(2).InfoS("Current time is within peak period", 
				"scheduleName", schedule.Name,
				"weekday", weekday,
				"currentTime", currentTime,
				"timezone", tzName,
				"schedule", fmt.Sprintf("%s from %s to %s", 
					describeDays(schedule.DayOfWeek), 
					schedule.StartTime, 
					schedule.EndTime))
			
			// Return the active period details
			return &schedulePeriod{
				schedule:    &schedule,
				localTime:   localTime,
				isPeakTime:  true,
				tzName:      tzName,
				weekday:     weekday,
				currentTime: currentTime,
			}
		}
	}

	klog.V(2).InfoS("Current time is NOT within any peak window", 
		"utcTime", now.Format("2006-01-02 15:04:05 MST"))
	
	return nil // Not in any peak time window
}

// IsPeakTime checks if the given time is within a peak time window
func (s *Scheduler) IsPeakTime(now time.Time) bool {
	period := s.findActivePeriod(now)
	return period != nil && period.isPeakTime
}

// IsCurrentlyPeakTime is deprecated, use IsPeakTime instead
func (s *Scheduler) IsCurrentlyPeakTime(now time.Time) bool {
	return s.IsPeakTime(now)
}

// GetCurrentRate returns the current electricity rate based on configured schedules
// Used mainly for metrics and reporting, not for scheduling decisions
func (s *Scheduler) GetCurrentRate(now time.Time) float64 {
	// If we don't have rates configured, return nominal values for peak/off-peak
	if len(s.config.Schedules) == 0 {
		return 0 // No schedules configured
	}

	// Default nominal values for metrics purposes
	peakRate := 1.0
	offPeakRate := 0.5
	
	// Check if we're in a peak period right now
	period := s.findActivePeriod(now)
	
	if period != nil && period.isPeakTime {
		// We found a matching peak period
		schedule := period.schedule
		
		// If this schedule has rates defined, use those
		if schedule.PeakRate > 0 && schedule.OffPeakRate > 0 {
			peakRate = schedule.PeakRate
			offPeakRate = schedule.OffPeakRate
		}
		
		return peakRate
	}
	
	// If we reach here, we're not in peak time, so return off-peak rate
	// First try to find a schedule that defines off-peak rate
	for _, schedule := range s.config.Schedules {
		if schedule.OffPeakRate > 0 {
			return schedule.OffPeakRate
		}
	}
	
	// Otherwise return default off-peak value
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
			// Find applicable schedule to check rates
			var peakRate float64 = 1.0 // Default nominal value
			
			// Try to find an appropriate peak rate from schedules
			for _, schedule := range s.config.Schedules {
				if schedule.PeakRate > 0 {
					peakRate = schedule.PeakRate
					break
				}
			}
			
			allowPeakScheduling = t >= peakRate
			
			klog.V(2).InfoS("Evaluated price threshold annotation",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"threshold", t,
				"peakRate", peakRate,
				"allowPeakScheduling", allowPeakScheduling)
		} else {
			klog.ErrorS(err, "Invalid price threshold annotation",
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"value", val)
			return framework.NewStatus(framework.Error, "invalid electricity price threshold annotation")
		}
	}
	
	// Find active peak period, if any
	period := s.findActivePeriod(now)
	
	// If we're in peak time and the pod isn't allowed to schedule during peak, block scheduling
	if period != nil && period.isPeakTime && !allowPeakScheduling {
		schedule := period.schedule
		days := describeDays(schedule.DayOfWeek)
		
		// Create a human-readable schedule description
		scheduleInfo := fmt.Sprintf("%s from %s to %s (%s, schedule: %s)", 
			days, schedule.StartTime, schedule.EndTime, period.tzName, schedule.Name)
		
		// Record this in metrics if rates are available 
		if schedule.PeakRate > 0 && schedule.OffPeakRate > 0 {
			savingsEstimate := schedule.PeakRate - schedule.OffPeakRate
			klog.V(2).InfoS("Estimated savings for delaying pod",
				"pod", klog.KObj(pod),
				"schedule", schedule.Name,
				"savingsPerKWh", savingsEstimate)
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
