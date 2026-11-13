package domain

import (
	"strings"
	"testing"
	"time"
)

func TestNewMessage(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr bool
	}{
		{name: "ふつうのテキスト", text: "こんにちは"},
		{name: "ちょうど上限", text: strings.Repeat("あ", MaxMessageLength)},
		{name: "上限を超える", text: strings.Repeat("あ", MaxMessageLength+1), wantErr: true},
		// 絵文字は UTF-16 で2と数えるので、上限の半分を少し超えるだけで上限を超える
		{name: "絵文字は2と数える", text: strings.Repeat("😀", MaxMessageLength/2+1), wantErr: true},
		{name: "空", text: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMessage(tt.text)
			if tt.wantErr {
				if err == nil {
					t.Fatal("NewMessage() succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewMessage(): %v", err)
			}
			if m.Text() != tt.text {
				t.Errorf("Text() = %q, want %q", m.Text(), tt.text)
			}
		})
	}
}

func TestComposeMessages(t *testing.T) {
	tokyo := mustLoadLocation(t, "Asia/Tokyo")
	date := mustDate(t, 2026, time.November, 12, tokyo) // 木曜日
	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, time.November, day, hour, minute, 0, 0, tokyo)
	}

	tests := []struct {
		name   string
		events []Event
		want   []string
	}{
		{
			name:   "予定がないときは送らない",
			events: nil,
			want:   nil,
		},
		{
			name: "終日予定を先に、時刻指定の予定は開始時刻の順に、複数のカレンダーを1通にまとめる",
			events: []Event{
				{Title: "歯医者", Start: at(12, 18, 30), End: at(12, 19, 0), CalendarLabel: "家族"},
				{Title: "打ち合わせ", Start: at(12, 9, 0), End: at(12, 10, 0), CalendarLabel: "仕事"},
				{Title: "誕生日", Start: at(12, 0, 0), End: at(13, 0, 0), AllDay: true, CalendarLabel: "家族"},
				{Title: "朝会", Start: at(12, 9, 0), End: at(12, 9, 15), CalendarLabel: "仕事"},
				{Title: "出張", Start: at(11, 0, 0), End: at(14, 0, 0), AllDay: true, CalendarLabel: "仕事"},
			},
			want: []string{
				"11/12(木) の予定\n" +
					"\n" +
					"[終日] 出張（仕事）\n" +
					"[終日] 誕生日（家族）\n" +
					"09:00-09:15 朝会（仕事）\n" +
					"09:00-10:00 打ち合わせ（仕事）\n" +
					"18:30-19:00 歯医者（家族）",
			},
		},
		{
			name: "日をまたぐ予定",
			events: []Event{
				{Title: "夜行バス", Start: at(11, 22, 0), End: at(12, 6, 0), CalendarLabel: "旅行"},
				{Title: "夜勤", Start: at(12, 22, 0), End: at(13, 7, 0), CalendarLabel: "仕事"},
				{Title: "合宿", Start: at(11, 10, 0), End: at(13, 17, 0), CalendarLabel: "部活"},
				{Title: "ちょうど日付が変わるまで", Start: at(12, 20, 0), End: at(13, 0, 0), CalendarLabel: "家族"},
			},
			want: []string{
				"11/12(木) の予定\n" +
					"\n" +
					"[終日] 合宿（部活）\n" +
					"〜06:00 夜行バス（旅行）\n" +
					"20:00-00:00 ちょうど日付が変わるまで（家族）\n" +
					"22:00〜 夜勤（仕事）",
			},
		},
		{
			name: "タイトルが空の予定、改行を含むタイトル、開始と終了が同じ予定",
			events: []Event{
				{Title: "", Start: at(12, 9, 0), End: at(12, 10, 0), CalendarLabel: "仕事"},
				{Title: "  ", Start: at(12, 11, 0), End: at(12, 12, 0), CalendarLabel: "仕事"},
				{Title: "買い物\nリスト", Start: at(12, 15, 0), End: at(12, 16, 0), CalendarLabel: "家族"},
				{Title: "リマインダー", Start: at(12, 17, 0), End: at(12, 17, 0), CalendarLabel: "家族"},
			},
			want: []string{
				"11/12(木) の予定\n" +
					"\n" +
					"09:00-10:00 (タイトルなし)（仕事）\n" +
					"11:00-12:00 (タイトルなし)（仕事）\n" +
					"15:00-16:00 買い物 リスト（家族）\n" +
					"17:00 リマインダー（家族）",
			},
		},
		{
			name: "予定の時刻を、対象日のタイムゾーンで表示する",
			events: []Event{
				// UTC の 00:00 は、Asia/Tokyo の 09:00
				{
					Title:         "UTC の予定",
					Start:         time.Date(2026, time.November, 12, 0, 0, 0, 0, time.UTC),
					End:           time.Date(2026, time.November, 12, 1, 0, 0, 0, time.UTC),
					CalendarLabel: "仕事",
				},
			},
			want: []string{
				"11/12(木) の予定\n" +
					"\n" +
					"09:00-10:00 UTC の予定（仕事）",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComposeMessages(DailySchedule{Date: date, Events: tt.events})
			assertMessages(t, got, tt.want)
		})
	}
}

func TestComposeMessagesSplitsAtEventLines(t *testing.T) {
	tokyo := mustLoadLocation(t, "Asia/Tokyo")
	date := mustDate(t, 2026, time.November, 12, tokyo)

	// 1行が約1000文字の予定を12件。1通目には5件、見出しが「（続き）」の分だけ長い2通目以降には4件入るので、
	// 5・4・3件の3通に分かれる
	title := strings.Repeat("あ", 980)
	var events []Event
	for i := range 12 {
		start := time.Date(2026, time.November, 12, i, 0, 0, 0, tokyo)
		events = append(events, Event{Title: title, Start: start, End: start.Add(30 * time.Minute), CalendarLabel: "仕事"})
	}

	got := ComposeMessages(DailySchedule{Date: date, Events: events})
	if len(got) != 3 {
		t.Fatalf("len(ComposeMessages()) = %d, want 3", len(got))
	}

	total := 0
	for i, m := range got {
		text := m.Text()
		if n := messageLength(text); n > MaxMessageLength {
			t.Errorf("messages[%d] の長さ = %d, want <= %d", i, n, MaxMessageLength)
		}

		wantHeading := "11/12(木) の予定"
		if i > 0 {
			wantHeading += "（続き）"
		}
		heading, body, ok := strings.Cut(text, "\n\n")
		if !ok || heading != wantHeading {
			t.Errorf("messages[%d] の見出し = %q, want %q", i, heading, wantHeading)
		}

		// 予定の行は、どれも切れずに1行のまま入っている
		for _, line := range strings.Split(body, "\n") {
			if !strings.HasSuffix(line, title+"（仕事）") {
				t.Errorf("messages[%d] に、途中で切れた予定の行がある: %q", i, line)
			}
			total++
		}
	}
	if total != len(events) {
		t.Errorf("予定の行の数 = %d, want %d", total, len(events))
	}
}

func TestComposeMessagesTruncatesTooLongEvent(t *testing.T) {
	tokyo := mustLoadLocation(t, "Asia/Tokyo")
	date := mustDate(t, 2026, time.November, 12, tokyo)
	start := time.Date(2026, time.November, 12, 9, 0, 0, 0, tokyo)

	// 1件だけで上限を超える予定（1通目に入る）と、そのあとに続く、同じく長い予定（2通目に入る）
	events := []Event{
		{Title: strings.Repeat("あ", MaxMessageLength), Start: start, End: start.Add(time.Hour), CalendarLabel: "仕事"},
		{Title: strings.Repeat("い", MaxMessageLength), Start: start.Add(time.Hour), End: start.Add(2 * time.Hour), CalendarLabel: "仕事"},
	}

	got := ComposeMessages(DailySchedule{Date: date, Events: events})
	if len(got) != 2 {
		t.Fatalf("len(ComposeMessages()) = %d, want 2", len(got))
	}
	for i, m := range got {
		if n := messageLength(m.Text()); n > MaxMessageLength {
			t.Errorf("messages[%d] の長さ = %d, want <= %d", i, n, MaxMessageLength)
		}
		if !strings.HasSuffix(m.Text(), ellipsis) {
			t.Errorf("messages[%d] が「%s」で終わっていない", i, ellipsis)
		}
	}
}

func assertMessages(t *testing.T, got []Message, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(messages) = %d, want %d\ngot: %q", len(got), len(want), texts(got))
	}
	for i := range want {
		if got[i].Text() != want[i] {
			t.Errorf("messages[%d]:\ngot:\n%s\nwant:\n%s", i, got[i].Text(), want[i])
		}
	}
}

func texts(messages []Message) []string {
	s := make([]string, 0, len(messages))
	for _, m := range messages {
		s = append(s, m.Text())
	}
	return s
}
