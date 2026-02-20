package scheduler

import (
	"testing"
	"time"
)

func TestParseTimeWindow(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
		want    TimeWindow
	}{
		{
			name: "valid weekdays",
			expr: "08:00-18:00 weekdays",
			want: TimeWindow{8, 0, 18, 0, "weekdays"},
		},
		{
			name: "overnight everyday",
			expr: "22:00-06:00 everyday",
			want: TimeWindow{22, 0, 6, 0, "everyday"},
		},
		{
			name: "weekends",
			expr: "10:30-15:45 weekends",
			want: TimeWindow{10, 30, 15, 45, "weekends"},
		},
		{
			name:    "missing day specifier",
			expr:    "08:00-18:00",
			wantErr: true,
		},
		{
			name:    "invalid day specifier",
			expr:    "08:00-18:00 holidays",
			wantErr: true,
		},
		{
			name:    "out of range hour",
			expr:    "25:00-18:00 weekdays",
			wantErr: true,
		},
		{
			name:    "out of range minute",
			expr:    "08:61-18:00 weekdays",
			wantErr: true,
		},
		{
			name:    "missing dash",
			expr:    "08:00 18:00 weekdays",
			wantErr: true,
		},
		{
			name:    "empty string",
			expr:    "",
			wantErr: true,
		},
		{
			name:    "garbage input",
			expr:    "not a time window",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTimeWindow(tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tt.expr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestIsActive(t *testing.T) {
	// Helper to create a time on a specific day and time.
	makeTime := func(weekday time.Weekday, hour, min int) time.Time {
		// Find a date that falls on the desired weekday.
		// Start from a known Monday (2026-01-05).
		base := time.Date(2026, 1, 5, hour, min, 0, 0, time.UTC) // Monday
		offset := int(weekday) - int(time.Monday)
		if offset < 0 {
			offset += 7
		}
		return base.AddDate(0, 0, offset)
	}

	tests := []struct {
		name   string
		tw     TimeWindow
		now    time.Time
		expect bool
	}{
		{
			name:   "inside normal range",
			tw:     TimeWindow{8, 0, 18, 0, "everyday"},
			now:    makeTime(time.Wednesday, 12, 0),
			expect: true,
		},
		{
			name:   "at start boundary",
			tw:     TimeWindow{8, 0, 18, 0, "everyday"},
			now:    makeTime(time.Wednesday, 8, 0),
			expect: true,
		},
		{
			name:   "at end boundary (exclusive)",
			tw:     TimeWindow{8, 0, 18, 0, "everyday"},
			now:    makeTime(time.Wednesday, 18, 0),
			expect: false,
		},
		{
			name:   "before range",
			tw:     TimeWindow{8, 0, 18, 0, "everyday"},
			now:    makeTime(time.Wednesday, 7, 59),
			expect: false,
		},
		{
			name:   "overnight wrap - before midnight",
			tw:     TimeWindow{22, 0, 6, 0, "everyday"},
			now:    makeTime(time.Wednesday, 23, 0),
			expect: true,
		},
		{
			name:   "overnight wrap - after midnight",
			tw:     TimeWindow{22, 0, 6, 0, "everyday"},
			now:    makeTime(time.Thursday, 3, 0),
			expect: true,
		},
		{
			name:   "overnight wrap - outside range",
			tw:     TimeWindow{22, 0, 6, 0, "everyday"},
			now:    makeTime(time.Wednesday, 12, 0),
			expect: false,
		},
		{
			name:   "weekdays on Monday",
			tw:     TimeWindow{8, 0, 18, 0, "weekdays"},
			now:    makeTime(time.Monday, 12, 0),
			expect: true,
		},
		{
			name:   "weekdays on Saturday",
			tw:     TimeWindow{8, 0, 18, 0, "weekdays"},
			now:    makeTime(time.Saturday, 12, 0),
			expect: false,
		},
		{
			name:   "weekdays on Sunday",
			tw:     TimeWindow{8, 0, 18, 0, "weekdays"},
			now:    makeTime(time.Sunday, 12, 0),
			expect: false,
		},
		{
			name:   "weekends on Saturday",
			tw:     TimeWindow{8, 0, 18, 0, "weekends"},
			now:    makeTime(time.Saturday, 12, 0),
			expect: true,
		},
		{
			name:   "weekends on Sunday",
			tw:     TimeWindow{8, 0, 18, 0, "weekends"},
			now:    makeTime(time.Sunday, 12, 0),
			expect: true,
		},
		{
			name:   "weekends on Friday",
			tw:     TimeWindow{8, 0, 18, 0, "weekends"},
			now:    makeTime(time.Friday, 12, 0),
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.tw.IsActive(tt.now)
			if got != tt.expect {
				t.Errorf("IsActive(%v) = %v, want %v", tt.now, got, tt.expect)
			}
		})
	}
}
