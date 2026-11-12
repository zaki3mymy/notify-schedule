package domain

import (
	"errors"
	"fmt"
	"time"
)

// Date は、タイムゾーン付きの日付（時刻を持たない）。
// 「その日」がいつからいつまでかは、タイムゾーンによって変わるので、タイムゾーンと組にして持つ。
type Date struct {
	year  int
	month time.Month
	day   int
	loc   *time.Location
}

// NewDate は、年・月・日とタイムゾーンから Date を作る。
// 存在しない日付（2月30日など）や、タイムゾーンがないときはエラーを返す。
func NewDate(year int, month time.Month, day int, loc *time.Location) (Date, error) {
	if loc == nil {
		return Date{}, errors.New("タイムゾーンが指定されていません")
	}
	t := time.Date(year, month, day, 0, 0, 0, 0, loc)
	if t.Year() != year || t.Month() != month || t.Day() != day {
		return Date{}, fmt.Errorf("存在しない日付です: %04d-%02d-%02d", year, month, day)
	}
	return Date{year: year, month: month, day: day, loc: loc}, nil
}

// Year は年を返す。
func (d Date) Year() int { return d.year }

// Month は月を返す。
func (d Date) Month() time.Month { return d.month }

// Day は日を返す。
func (d Date) Day() int { return d.day }

// Location はタイムゾーンを返す。
func (d Date) Location() *time.Location { return d.loc }

// Weekday は曜日を返す。
func (d Date) Weekday() time.Weekday { return d.Start().Weekday() }

// Start は、その日の始まり（そのタイムゾーンでの 00:00）を返す。
func (d Date) Start() time.Time {
	return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, d.loc)
}

// End は、その日の終わり（翌日の 00:00）を返す。その日は [Start, End) の範囲。
func (d Date) End() time.Time {
	return time.Date(d.year, d.month, d.day+1, 0, 0, 0, 0, d.loc)
}

// String は YYYY-MM-DD の形で返す。
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day)
}
