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

func TestParseDate(t *testing.T) {
	tokyo := mustLoadLocation(t, "Asia/Tokyo")

	tests := []struct {
		name    string
		s       string
		want    string
		wantErr bool
	}{
		{name: "YYYY-MM-DD", s: "2026-11-12", want: "2026-11-12"},
		{name: "うるう年の2月29日", s: "2028-02-29", want: "2028-02-29"},
		{name: "存在しない日付", s: "2026-02-30", wantErr: true},
		{name: "13月", s: "2026-13-01", wantErr: true},
		{name: "桁が足りない", s: "2026-1-2", wantErr: true},
		{name: "区切りが違う", s: "2026/11/12", wantErr: true},
		{name: "時刻付き", s: "2026-11-12T09:00:00+09:00", wantErr: true},
		{name: "前後に空白", s: " 2026-11-12 ", wantErr: true},
		{name: "空", s: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := ParseDate(tt.s, tokyo)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDate(%q) = %v, want error", tt.s, d)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDate(%q): %v", tt.s, err)
			}
			if d.String() != tt.want || d.Location() != tokyo {
				t.Errorf("ParseDate(%q) = %v (%v), want %s (%v)", tt.s, d, d.Location(), tt.want, tokyo)
			}
		})
	}

	if _, err := ParseDate("2026-11-12", nil); err == nil {
		t.Error("タイムゾーンがないのに ParseDate() が成功した")
	}
}

func TestDateFromNow(t *testing.T) {
	tokyo := mustLoadLocation(t, "Asia/Tokyo")

	tests := []struct {
		name       string
		now        time.Time
		loc        *time.Location
		offsetDays int
		want       string
	}{
		{
			name:       "明日",
			now:        time.Date(2026, time.November, 12, 21, 0, 0, 0, tokyo),
			loc:        tokyo,
			offsetDays: 1,
			want:       "2026-11-13",
		},
		{
			name:       "今日",
			now:        time.Date(2026, time.November, 12, 7, 0, 0, 0, tokyo),
			loc:        tokyo,
			offsetDays: 0,
			want:       "2026-11-12",
		},
		{
			name:       "昨日（負の値）",
			now:        time.Date(2026, time.November, 12, 7, 0, 0, 0, tokyo),
			loc:        tokyo,
			offsetDays: -1,
			want:       "2026-11-11",
		},
		{
			// UTC の 11/11 16:00 は、Asia/Tokyo の 11/12 01:00。「今日」は Asia/Tokyo で決める
			name:       "UTC では前日だが、Asia/Tokyo では当日",
			now:        time.Date(2026, time.November, 11, 16, 0, 0, 0, time.UTC),
			loc:        tokyo,
			offsetDays: 1,
			want:       "2026-11-13",
		},
		{
			name:       "UTC で考えると前日になる時刻を、UTC のタイムゾーンで",
			now:        time.Date(2026, time.November, 11, 16, 0, 0, 0, time.UTC),
			loc:        time.UTC,
			offsetDays: 1,
			want:       "2026-11-12",
		},
		{
			name:       "月末をまたぐ",
			now:        time.Date(2026, time.November, 30, 21, 0, 0, 0, tokyo),
			loc:        tokyo,
			offsetDays: 1,
			want:       "2026-12-01",
		},
		{
			name:       "年末をまたぐ",
			now:        time.Date(2026, time.December, 31, 21, 0, 0, 0, tokyo),
			loc:        tokyo,
			offsetDays: 1,
			want:       "2027-01-01",
		},
		{
			name:       "うるう年の2月28日の翌日",
			now:        time.Date(2028, time.February, 28, 21, 0, 0, 0, tokyo),
			loc:        tokyo,
			offsetDays: 1,
			want:       "2028-02-29",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := DateFromNow(tt.now, tt.loc, tt.offsetDays)
			if err != nil {
				t.Fatalf("DateFromNow(): %v", err)
			}
			if d.String() != tt.want || d.Location() != tt.loc {
				t.Errorf("DateFromNow() = %v (%v), want %s (%v)", d, d.Location(), tt.want, tt.loc)
			}
		})
	}

	if _, err := DateFromNow(time.Now(), nil, 1); err == nil {
		t.Error("タイムゾーンがないのに DateFromNow() が成功した")
	}
}

// FuzzParseDate は、どんな文字列でも ParseDate がパニックせず、成功したときは
// String() で元の文字列に戻り、NewDate でも同じ日付が作れることを確かめる。
//
//	go test -fuzz=FuzzParseDate ./internal/domain
func FuzzParseDate(f *testing.F) {
	for _, s := range []string{
		"2026-11-12", "2028-02-29", "2026-02-30", "0000-01-01", "9999-12-31",
		"2026-1-2", "2026/11/12", " 2026-11-12", "2026-11-12\n", "", "２０２６-１１-１２", "\xff",
	} {
		f.Add(s)
	}

	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		f.Fatalf("LoadLocation: %v", err)
	}

	f.Fuzz(func(t *testing.T, s string) {
		d, err := ParseDate(s, loc)
		if err != nil {
			return
		}
		if d.String() != s {
			t.Fatalf("ParseDate(%q).String() = %q, want %q", s, d.String(), s)
		}
		if d.Location() != loc {
			t.Fatalf("ParseDate(%q).Location() = %v, want %v", s, d.Location(), loc)
		}
		again, err := NewDate(d.Year(), d.Month(), d.Day(), loc)
		if err != nil || again != d {
			t.Fatalf("NewDate(%d, %d, %d) = %v, %v, want %v", d.Year(), d.Month(), d.Day(), again, err, d)
		}
	})
}
