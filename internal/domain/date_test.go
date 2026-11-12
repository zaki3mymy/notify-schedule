package domain

import (
	"testing"
	"time"
)

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func mustDate(t *testing.T, year int, month time.Month, day int, loc *time.Location) Date {
	t.Helper()
	d, err := NewDate(year, month, day, loc)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	return d
}

func TestNewDate(t *testing.T) {
	tokyo := mustLoadLocation(t, "Asia/Tokyo")

	tests := []struct {
		name    string
		year    int
		month   time.Month
		day     int
		loc     *time.Location
		wantErr bool
	}{
		{name: "正しい日付", year: 2026, month: time.November, day: 12, loc: tokyo},
		{name: "うるう年の2月29日", year: 2028, month: time.February, day: 29, loc: tokyo},
		{name: "うるう年でない2月29日", year: 2026, month: time.February, day: 29, loc: tokyo, wantErr: true},
		{name: "13月", year: 2026, month: 13, day: 1, loc: tokyo, wantErr: true},
		{name: "0日", year: 2026, month: time.November, day: 0, loc: tokyo, wantErr: true},
		{name: "タイムゾーンがない", year: 2026, month: time.November, day: 12, loc: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewDate(tt.year, tt.month, tt.day, tt.loc)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewDate() = %v, want error", d)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewDate(): %v", err)
			}
			if d.Year() != tt.year || d.Month() != tt.month || d.Day() != tt.day || d.Location() != tt.loc {
				t.Errorf("NewDate() = %v (%v), want %04d-%02d-%02d (%v)", d, d.Location(), tt.year, tt.month, tt.day, tt.loc)
			}
		})
	}
}

func TestDateRange(t *testing.T) {
	tokyo := mustLoadLocation(t, "Asia/Tokyo")

	tests := []struct {
		name      string
		date      Date
		wantStart string
		wantEnd   string
	}{
		{
			name:      "その日の 00:00 から翌日の 00:00 まで（タイムゾーン付き）",
			date:      mustDate(t, 2026, time.November, 12, tokyo),
			wantStart: "2026-11-12T00:00:00+09:00",
			wantEnd:   "2026-11-13T00:00:00+09:00",
		},
		{
			name:      "月末",
			date:      mustDate(t, 2026, time.November, 30, tokyo),
			wantStart: "2026-11-30T00:00:00+09:00",
			wantEnd:   "2026-12-01T00:00:00+09:00",
		},
		{
			name:      "年末",
			date:      mustDate(t, 2026, time.December, 31, tokyo),
			wantStart: "2026-12-31T00:00:00+09:00",
			wantEnd:   "2027-01-01T00:00:00+09:00",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.date.Start().Format(time.RFC3339); got != tt.wantStart {
				t.Errorf("Start() = %s, want %s", got, tt.wantStart)
			}
			if got := tt.date.End().Format(time.RFC3339); got != tt.wantEnd {
				t.Errorf("End() = %s, want %s", got, tt.wantEnd)
			}
		})
	}
}

func TestDateWeekdayAndString(t *testing.T) {
	d := mustDate(t, 2026, time.November, 12, mustLoadLocation(t, "Asia/Tokyo"))
	if got := d.Weekday(); got != time.Thursday {
		t.Errorf("Weekday() = %v, want %v", got, time.Thursday)
	}
	if got := d.String(); got != "2026-11-12" {
		t.Errorf("String() = %q, want %q", got, "2026-11-12")
	}
}
