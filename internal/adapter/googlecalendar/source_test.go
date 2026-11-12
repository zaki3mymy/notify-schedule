package googlecalendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"github.com/zaki3mymy/notify-schedule/internal/domain"
	"github.com/zaki3mymy/notify-schedule/internal/usecase"
)

// Source が usecase.ScheduleSource を満たすこと
var _ usecase.ScheduleSource = (*Source)(nil)

// fakeCalendar は、Calendar API の events.list を真似る偽サーバー。
// pages[カレンダー ID] は、ページごとの予定（JSON の items）。2ページ目以降は nextPageToken で返す。
type fakeCalendar struct {
	pages map[string][][]map[string]any
	// failing のカレンダー ID には、500 を返す
	failing map[string]bool

	mu      sync.Mutex
	queries []url.Values
	ids     []string
}

func (f *fakeCalendar) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// パスは /calendars/{calendarId}/events
	id, ok := strings.CutPrefix(r.URL.Path, "/calendars/")
	id, ok2 := strings.CutSuffix(id, "/events")
	if !ok || !ok2 {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	f.queries = append(f.queries, r.URL.Query())
	f.ids = append(f.ids, id)
	f.mu.Unlock()

	if f.failing[id] {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": {"code": 500, "message": "backend error"}}`))
		return
	}

	pages := f.pages[id]
	index := 0
	if token := r.URL.Query().Get("pageToken"); token != "" {
		n, err := strconv.Atoi(token)
		if err != nil {
			http.Error(w, "bad token", http.StatusBadRequest)
			return
		}
		index = n
	}
	resp := map[string]any{"items": []map[string]any{}}
	if index < len(pages) {
		resp["items"] = pages[index]
	}
	if index+1 < len(pages) {
		resp["nextPageToken"] = strconv.Itoa(index + 1)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func newSource(t *testing.T, f *fakeCalendar) *Source {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	s, err := New(context.Background(),
		option.WithEndpoint(srv.URL+"/"),
		option.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s
}

func testDate(t *testing.T) domain.Date {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	d, err := domain.NewDate(2026, time.November, 13, loc)
	if err != nil {
		t.Fatalf("NewDate: %v", err)
	}
	return d
}

func timed(id, summary, start, end string) map[string]any {
	return map[string]any{
		"id": id, "status": "confirmed", "summary": summary,
		"start": map[string]any{"dateTime": start}, "end": map[string]any{"dateTime": end},
	}
}

func allDay(id, summary, start, end string) map[string]any {
	return map[string]any{
		"id": id, "status": "confirmed", "summary": summary,
		"start": map[string]any{"date": start}, "end": map[string]any{"date": end},
	}
}

func TestFetch(t *testing.T) {
	d := testDate(t)
	f := &fakeCalendar{pages: map[string][][]map[string]any{
		"work@example.com": {
			// 1ページ目
			{
				timed("w1", "打ち合わせ", "2026-11-13T09:00:00+09:00", "2026-11-13T10:00:00+09:00"),
				{"id": "w2", "status": "cancelled"}, // キャンセルされた予定（中身がないこともある）
			},
			// 2ページ目
			{
				timed("w3", "夜勤", "2026-11-13T22:00:00+09:00", "2026-11-14T07:00:00+09:00"),
			},
		},
		"family@example.com": {
			{
				allDay("f1", "誕生日", "2026-11-13", "2026-11-14"),
				timed("f2", "UTC の予定", "2026-11-13T00:00:00Z", "2026-11-13T01:00:00Z"),
			},
		},
	}}
	s := newSource(t, f)
	cals := []domain.Calendar{
		{ID: "work@example.com", Label: "仕事"},
		{ID: "family@example.com", Label: "家族"},
	}

	got, err := s.Fetch(context.Background(), cals, d)
	if err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if got.Date != d {
		t.Errorf("Date = %v, want %v", got.Date, d)
	}

	tokyo := d.Location()
	want := []domain.Event{
		{Title: "打ち合わせ", Start: time.Date(2026, 11, 13, 9, 0, 0, 0, tokyo), End: time.Date(2026, 11, 13, 10, 0, 0, 0, tokyo), CalendarLabel: "仕事"},
		{Title: "夜勤", Start: time.Date(2026, 11, 13, 22, 0, 0, 0, tokyo), End: time.Date(2026, 11, 14, 7, 0, 0, 0, tokyo), CalendarLabel: "仕事"},
		{Title: "誕生日", Start: time.Date(2026, 11, 13, 0, 0, 0, 0, tokyo), End: time.Date(2026, 11, 14, 0, 0, 0, 0, tokyo), AllDay: true, CalendarLabel: "家族"},
		{Title: "UTC の予定", Start: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC), CalendarLabel: "家族"},
	}
	if len(got.Events) != len(want) {
		t.Fatalf("len(Events) = %d, want %d: %+v", len(got.Events), len(want), got.Events)
	}
	for i := range want {
		g, w := got.Events[i], want[i]
		if g.Title != w.Title || !g.Start.Equal(w.Start) || !g.End.Equal(w.End) || g.AllDay != w.AllDay || g.CalendarLabel != w.CalendarLabel {
			t.Errorf("Events[%d] = %+v, want %+v", i, g, w)
		}
	}

	// work は2ページ、family は1ページで、合わせて3回呼ばれる
	if len(f.queries) != 3 {
		t.Fatalf("リクエストの数 = %d, want 3", len(f.queries))
	}
	for i, q := range f.queries {
		wantParams := map[string]string{
			"singleEvents": "true",
			"orderBy":      "startTime",
			"timeMin":      "2026-11-13T00:00:00+09:00",
			"timeMax":      "2026-11-14T00:00:00+09:00",
			"timeZone":     "Asia/Tokyo",
			"maxResults":   "250",
		}
		for k, v := range wantParams {
			if q.Get(k) != v {
				t.Errorf("リクエスト[%d] の %s = %q, want %q", i, k, q.Get(k), v)
			}
		}
	}
	if f.queries[1].Get("pageToken") != "1" {
		t.Errorf("2ページ目の pageToken = %q, want %q", f.queries[1].Get("pageToken"), "1")
	}
}

func TestFetchNoEvents(t *testing.T) {
	s := newSource(t, &fakeCalendar{pages: map[string][][]map[string]any{}})
	got, err := s.Fetch(context.Background(), []domain.Calendar{{ID: "empty@example.com", Label: "空"}}, testDate(t))
	if err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if !got.IsEmpty() {
		t.Errorf("予定がないのに Events = %+v", got.Events)
	}
}

func TestFetchErrors(t *testing.T) {
	d := testDate(t)
	f := &fakeCalendar{
		pages: map[string][][]map[string]any{
			"ok@example.com": {{timed("o1", "予定", "2026-11-13T09:00:00+09:00", "2026-11-13T10:00:00+09:00")}},
			"broken@example.com": {{
				{"id": "b1", "status": "confirmed", "summary": "開始がない予定"},
			}},
		},
		failing: map[string]bool{"down1@example.com": true, "down2@example.com": true},
	}
	s := newSource(t, f)
	cals := []domain.Calendar{
		{ID: "down1@example.com", Label: "落ちている1"},
		{ID: "ok@example.com", Label: "正常"},
		{ID: "broken@example.com", Label: "壊れた予定"},
		{ID: "down2@example.com", Label: "落ちている2"},
	}

	got, err := s.Fetch(context.Background(), cals, d)
	if err == nil {
		t.Fatalf("Fetch() = %+v, want error", got)
	}
	msg := err.Error()
	// 失敗したカレンダーは、すべてラベルでわかる
	for _, label := range []string{"落ちている1", "壊れた予定", "落ちている2"} {
		if !strings.Contains(msg, "「"+label+"」") {
			t.Errorf("エラーにカレンダー「%s」が含まれていない: %q", label, msg)
		}
	}
	if strings.Contains(msg, "「正常」") {
		t.Errorf("成功したカレンダーがエラーに含まれている: %q", msg)
	}
	// カレンダーの ID（メールアドレスのことがある）はエラーに含めない
	if strings.Contains(msg, "@example.com") {
		t.Errorf("エラーにカレンダーの ID が含まれている: %q", msg)
	}
	// 1つ失敗しても、ほかのカレンダーも試す
	if len(f.ids) != 4 {
		t.Errorf("リクエストしたカレンダーの数 = %d, want 4（%v）", len(f.ids), f.ids)
	}
}

func TestToEventInvalidTimes(t *testing.T) {
	tokyo := testDate(t).Location()
	tests := []struct {
		name string
		item map[string]any
	}{
		{name: "終日予定の開始日が不正", item: allDay("x", "a", "2026/11/13", "2026-11-14")},
		{name: "終日予定の終了日が不正", item: allDay("x", "a", "2026-11-13", "")},
		{name: "開始時刻が不正", item: timed("x", "a", "09:00", "2026-11-13T10:00:00+09:00")},
		{name: "終了時刻が不正", item: timed("x", "a", "2026-11-13T09:00:00+09:00", "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.item)
			if err != nil {
				t.Fatal(err)
			}
			var item calendar.Event
			if err := json.Unmarshal(b, &item); err != nil {
				t.Fatal(err)
			}
			if _, err := toEvent(&item, "ラベル", tokyo); err == nil {
				t.Error("toEvent() succeeded, want error")
			}
		})
	}
}
