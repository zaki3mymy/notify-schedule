package domain

import (
	"slices"
	"time"
)

// Calendar は、予定を取得するカレンダー。Label は通知のときに予定に添える名前。
type Calendar struct {
	ID    string
	Label string
}

// Event は、カレンダーの予定の1件。
//
// 終日予定（AllDay）の Start・End は、その予定の日付の 00:00 を表す（End は最終日の翌日）。
// 時刻指定の予定の Start・End は、その予定の時刻を表す。End が Start より前の予定は、Start で終わるものとして扱う。
type Event struct {
	Title         string
	Start         time.Time
	End           time.Time
	AllDay        bool
	CalendarLabel string
}

// DailySchedule は、ある1日の予定。
type DailySchedule struct {
	Date   Date
	Events []Event
}

// IsEmpty は、予定が1件もないかを返す。
func (s DailySchedule) IsEmpty() bool {
	return len(s.Events) == 0
}

// SortedEvents は、通知に載せる順に並べた予定を返す（Events は変えない）。
//
//  1. その日全体にかかる予定（終日予定と、前日から翌日まで続く時刻指定の予定）を先に
//  2. それ以外の時刻指定の予定は、開始時刻の順（同じなら終了時刻の順）
//
// 同じ順のものは、カレンダーのラベル、タイトルの順で並べる（実行のたびに順番が変わらないようにするため）。
func (s DailySchedule) SortedEvents() []Event {
	events := slices.Clone(s.Events)
	slices.SortStableFunc(events, func(a, b Event) int {
		aWhole, bWhole := s.coversWholeDay(a), s.coversWholeDay(b)
		switch {
		case aWhole && !bWhole:
			return -1
		case !aWhole && bWhole:
			return 1
		}
		if !aWhole {
			if c := a.Start.Compare(b.Start); c != 0 {
				return c
			}
			if c := a.End.Compare(b.End); c != 0 {
				return c
			}
		}
		if a.CalendarLabel != b.CalendarLabel {
			if a.CalendarLabel < b.CalendarLabel {
				return -1
			}
			return 1
		}
		switch {
		case a.Title < b.Title:
			return -1
		case a.Title > b.Title:
			return 1
		}
		return 0
	})
	return events
}

// coversWholeDay は、予定がその日全体にかかるか（終日予定か、その日の始まりより前に始まり、
// その日の終わり以降に終わる時刻指定の予定か）を返す。
func (s DailySchedule) coversWholeDay(e Event) bool {
	if e.AllDay {
		return true
	}
	return e.Start.Before(s.Date.Start()) && !e.End.Before(s.Date.End())
}
