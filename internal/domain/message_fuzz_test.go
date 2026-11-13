package domain

import (
	"strings"
	"testing"
	"time"
)

// FuzzComposeMessages は、予定のタイトル・ラベル・数・時刻に何が来ても、次の性質が成り立つことを確かめる。
//
//   - パニックしない
//   - 予定が1件以上あれば、メッセージが1通以上ある
//   - どのメッセージも空でなく、MaxMessageLength を超えない
//   - 1通目の見出しは「M/D(曜) の予定」、2通目からは「（続き）」付き
//   - すべてのメッセージの予定の行を合わせると、予定の数と同じ（予定が消えたり、行が崩れたりしない）
//
// go test ではシードだけを実行する。入力を生成して探すときは次を実行する。
//
//	go test -fuzz=FuzzComposeMessages ./internal/domain
func FuzzComposeMessages(f *testing.F) {
	// title, label, 予定の数, タイトルの繰り返し回数, 開始の分（対象日の 00:00 から）, 長さの分, 終日か
	f.Add("打ち合わせ", "仕事", uint8(1), uint16(1), int16(540), int16(60), false)
	f.Add("", "", uint8(3), uint16(0), int16(0), int16(0), true)
	f.Add("買い物\nリスト", "家\n族", uint8(2), uint16(1), int16(-120), int16(600), false)
	f.Add("あ", "仕事", uint8(12), uint16(980), int16(0), int16(30), false)     // 予定の行の区切りで分かれる
	f.Add("😀", "仕事", uint8(2), uint16(3000), int16(1380), int16(120), false) // サロゲートペアで1件が上限を超える
	f.Add("  \t ", "　", uint8(1), uint16(1), int16(1440), int16(-10), false)
	f.Add("\xff\xfe", "\x80", uint8(5), uint16(100), int16(-1440), int16(3000), false) // 不正な UTF-8

	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		f.Fatalf("LoadLocation: %v", err)
	}
	date, err := NewDate(2026, time.November, 12, loc)
	if err != nil {
		f.Fatalf("NewDate: %v", err)
	}

	f.Fuzz(func(t *testing.T, title, label string, count uint8, repeat uint16, startMinute, durationMinute int16, allDay bool) {
		// 大きすぎる入力でテストが遅くならないよう、範囲を絞る
		n := int(count)%30 + 1
		title = strings.Repeat(title, int(repeat)%3000)
		if len(title) > 4*MaxMessageLength*2 {
			title = title[:4*MaxMessageLength*2]
		}

		var events []Event
		for i := range n {
			start := date.Start().Add(time.Duration(int(startMinute)+i*15) * time.Minute)
			events = append(events, Event{
				Title:         title,
				Start:         start,
				End:           start.Add(time.Duration(durationMinute) * time.Minute),
				AllDay:        allDay,
				CalendarLabel: label,
			})
		}

		messages := ComposeMessages(DailySchedule{Date: date, Events: events})
		if len(messages) == 0 {
			t.Fatalf("予定が %d 件あるのに、メッセージがない", n)
		}

		lines := 0
		for i, m := range messages {
			text := m.Text()
			if text == "" {
				t.Fatalf("messages[%d] が空", i)
			}
			if l := messageLength(text); l > MaxMessageLength {
				t.Fatalf("messages[%d] の長さ = %d, want <= %d", i, l, MaxMessageLength)
			}

			wantHeading := "11/12(木) の予定"
			if i > 0 {
				wantHeading += continuedMark
			}
			heading, body, ok := strings.Cut(text, "\n\n")
			if !ok || heading != wantHeading {
				t.Fatalf("messages[%d] の見出し = %q, want %q", i, heading, wantHeading)
			}
			if body == "" {
				t.Fatalf("messages[%d] に予定の行がない", i)
			}
			lines += strings.Count(body, "\n") + 1
		}
		if lines != n {
			t.Fatalf("予定の行の数 = %d, want %d", lines, n)
		}
	})
}
