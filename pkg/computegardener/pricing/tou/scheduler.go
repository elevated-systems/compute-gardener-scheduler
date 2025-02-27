package tou

import (
	"fmt"
	"time"

	"sigs.k8s.io/scheduler-plugins/pkg/computegardener/config"
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

// containsDay checks if a day is included in a day string (e.g. "1,2,3" contains "2")
func containsDay(days string, day string) bool {
	for _, d := range days {
		if string(d) == day {
			return true
		}
	}
	return false
}
