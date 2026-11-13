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

// dateLayout は、日付の文字列の形（YYYY-MM-DD）。
const dateLayout = "2006-01-02"

// ParseDate は、YYYY-MM-DD の形の文字列とタイムゾーンから Date を作る。
// 形が違うとき（桁が足りない、区切りが違う、前後に余計な文字がある）や、存在しない日付のときはエラーを返す。
func ParseDate(s string, loc *time.Location) (Date, error) {
	if loc == nil {
		return Date{}, errors.New("タイムゾーンが指定されていません")
	}
	t, err := time.ParseInLocation(dateLayout, s, loc)
	if err != nil {
		return Date{}, fmt.Errorf("日付は YYYY-MM-DD の形で指定してください: %q", s)
	}
	return NewDate(t.Year(), t.Month(), t.Day(), loc)
}

// DateFromNow は、now のそのタイムゾーンでの日付から offsetDays 日後の Date を返す（負の値なら前）。
// 例えば offsetDays が 1 なら「そのタイムゾーンでの明日」。now は引数で受け取る（テストで固定できるようにするため）。
func DateFromNow(now time.Time, loc *time.Location, offsetDays int) (Date, error) {
	if loc == nil {
		return Date{}, errors.New("タイムゾーンが指定されていません")
	}
	today := now.In(loc)
	// time.Date は日の繰り上がり（月末・年末をまたぐ場合）を正規化する
	t := time.Date(today.Year(), today.Month(), today.Day()+offsetDays, 0, 0, 0, 0, loc)
	return NewDate(t.Year(), t.Month(), t.Day(), loc)
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
// 予定の取得元に渡す時間範囲にも、Start と End をそのまま使う。
func (d Date) End() time.Time {
	return time.Date(d.year, d.month, d.day+1, 0, 0, 0, 0, d.loc)
}

// String は YYYY-MM-DD の形で返す（ParseDate で読める形）。
func (d Date) String() string {
	return d.Start().Format(dateLayout)
}
