package domain

import (
	"slices"
	"testing"
	"time"
)

func TestDailyScheduleIsEmpty(t *testing.T) {
	date := mustDate(t, 2026, time.November, 12, mustLoadLocation(t, "Asia/Tokyo"))
	if !(DailySchedule{Date: date}).IsEmpty() {
		t.Error("予定がないのに IsEmpty() = false")
	}
	if (DailySchedule{Date: date, Events: []Event{{Title: "a"}}}).IsEmpty() {
		t.Error("予定があるのに IsEmpty() = true")
	}
}

func TestDailyScheduleSortedEvents(t *testing.T) {
	tokyo := mustLoadLocation(t, "Asia/Tokyo")
	date := mustDate(t, 2026, time.November, 12, tokyo)
	at := func(hour int) time.Time { return time.Date(2026, time.November, 12, hour, 0, 0, 0, tokyo) }

	events := []Event{
		{Title: "c", Start: at(10), End: at(11), CalendarLabel: "仕事"},
		{Title: "b", Start: at(9), End: at(11), CalendarLabel: "仕事"},
		{Title: "a", Start: at(9), End: at(10), CalendarLabel: "仕事"},
		{Title: "終日2", Start: at(0), End: at(24), AllDay: true, CalendarLabel: "家族"},
		{Title: "終日1", Start: at(0), End: at(24), AllDay: true, CalendarLabel: "仕事"},
		{Title: "同じ時刻の b", Start: at(12), End: at(13), CalendarLabel: "家族"},
		{Title: "同じ時刻の a", Start: at(12), End: at(13), CalendarLabel: "仕事"},
	}
	schedule := DailySchedule{Date: date, Events: events}
	original := slices.Clone(events)

	var got []string
	for _, e := range schedule.SortedEvents() {
		got = append(got, e.Title)
	}
	// 終日はラベル順（仕事・家族）、時刻指定は開始→終了の順、同じ時刻ならラベル順
	want := []string{"終日1", "終日2", "a", "b", "c", "同じ時刻の a", "同じ時刻の b"}
	if !slices.Equal(got, want) {
		t.Errorf("SortedEvents() = %q, want %q", got, want)
	}
	if !slices.Equal(schedule.Events, original) {
		t.Error("SortedEvents() が Events を書き換えた")
	}
}
