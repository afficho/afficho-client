package scheduler

import (
	"fmt"
	"strings"
	"time"
)

// TimeWindow represents a recurring time window during which a schedule is active.
type TimeWindow struct {
	StartHour, StartMin int
	EndHour, EndMin     int
	Days                string // "everyday" | "weekdays" | "weekends"
}

// ParseTimeWindow parses a time-window expression like "08:00-18:00 weekdays".
// Supported day specifiers: everyday, weekdays, weekends.
func ParseTimeWindow(expr string) (TimeWindow, error) {
	parts := strings.Fields(expr)
	if len(parts) != 2 {
		return TimeWindow{}, fmt.Errorf("expected 'HH:MM-HH:MM days', got %q", expr)
	}

	timeRange := parts[0]
	days := strings.ToLower(parts[1])

	switch days {
	case "everyday", "weekdays", "weekends":
	default:
		return TimeWindow{}, fmt.Errorf("unsupported day specifier %q (use everyday, weekdays, or weekends)", days)
	}

	times := strings.SplitN(timeRange, "-", 2)
	if len(times) != 2 {
		return TimeWindow{}, fmt.Errorf("expected HH:MM-HH:MM, got %q", timeRange)
	}

	sh, sm, err := parseHHMM(times[0])
	if err != nil {
		return TimeWindow{}, fmt.Errorf("start time: %w", err)
	}
	eh, em, err := parseHHMM(times[1])
	if err != nil {
		return TimeWindow{}, fmt.Errorf("end time: %w", err)
	}

	return TimeWindow{
		StartHour: sh, StartMin: sm,
		EndHour: eh, EndMin: em,
		Days: days,
	}, nil
}

// IsActive returns true if the given time falls within this window.
func (tw TimeWindow) IsActive(now time.Time) bool {
	if !tw.matchesDay(now.Weekday()) {
		return false
	}

	nowMin := now.Hour()*60 + now.Minute()
	startMin := tw.StartHour*60 + tw.StartMin
	endMin := tw.EndHour*60 + tw.EndMin

	if startMin <= endMin {
		// Normal range: e.g. 08:00-18:00
		return nowMin >= startMin && nowMin < endMin
	}
	// Wraps past midnight: e.g. 22:00-06:00
	return nowMin >= startMin || nowMin < endMin
}

func (tw TimeWindow) matchesDay(day time.Weekday) bool {
	switch tw.Days {
	case "everyday":
		return true
	case "weekdays":
		return day >= time.Monday && day <= time.Friday
	case "weekends":
		return day == time.Saturday || day == time.Sunday
	default:
		return false
	}
}

func parseHHMM(s string) (hour, minute int, err error) {
	var h, m int
	n, err := fmt.Sscanf(s, "%d:%d", &h, &m)
	if err != nil || n != 2 {
		return 0, 0, fmt.Errorf("invalid time %q (expected HH:MM)", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("time %q out of range", s)
	}
	return h, m, nil
}
