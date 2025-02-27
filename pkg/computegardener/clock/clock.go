package clock

import "time"

// Clock is an interface that wraps time functions to make them testable
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
}

// RealClock implements Clock interface with actual time
type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

func (RealClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

// MockClock implements Clock interface for testing
type MockClock struct {
	now time.Time
}

func NewMockClock(t time.Time) *MockClock {
	return &MockClock{now: t}
}

func (m *MockClock) Now() time.Time {
	return m.now
}

func (m *MockClock) Since(t time.Time) time.Duration {
	return m.now.Sub(t)
}

func (m *MockClock) Set(t time.Time) {
	m.now = t
}
