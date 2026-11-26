package googlecalendar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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

	// release が nil でなければ、閉じられるまで応答を待たせる（同時に処理中のリクエストを数えるのに使う）
	release chan struct{}

	mu       sync.Mutex
	queries  []url.Values
	ids      []string
	inFlight int
}

func (f *fakeCalendar) currentInFlight() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inFlight
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
	f.inFlight++
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	if f.release != nil {
		<-f.release
	}

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

	fetched, err := s.Fetch(context.Background(), cals, d)
	if err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if len(fetched.Failures) != 0 {
		t.Errorf("Failures = %v, want なし", fetched.Failures)
	}
	got := fetched.Schedule
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

	// work は2ページ、family は1ページで、合わせて3回呼ばれる（カレンダーは並行して取得するので、順は決まらない）
	if len(f.queries) != 3 {
		t.Fatalf("リクエストの数 = %d, want 3", len(f.queries))
	}
	var workTokens []string
	for i, id := range f.ids {
		if id == "work@example.com" {
			workTokens = append(workTokens, f.queries[i].Get("pageToken"))
		}
	}
	if want := []string{"", "1"}; !slices.Equal(workTokens, want) {
		t.Errorf("work の pageToken = %q, want %q", workTokens, want)
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
}

func TestFetchNoEvents(t *testing.T) {
	s := newSource(t, &fakeCalendar{pages: map[string][][]map[string]any{}})
	got, err := s.Fetch(context.Background(), []domain.Calendar{{ID: "empty@example.com", Label: "空"}}, testDate(t))
	if err != nil {
		t.Fatalf("Fetch(): %v", err)
	}
	if !got.Schedule.IsEmpty() || len(got.Failures) != 0 {
		t.Errorf("予定がないのに Events = %+v, Failures = %v", got.Schedule.Events, got.Failures)
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
	if err != nil {
		t.Fatalf("Fetch(): %v（失敗したカレンダーは Failures で返す）", err)
	}
	// 失敗したカレンダーは、すべて Failures に入る（cals の順）
	var labels []string
	for _, f := range got.Failures {
		labels = append(labels, f.Calendar.Label)
		if f.Err == nil {
			t.Errorf("カレンダー「%s」の Err が nil", f.Calendar.Label)
		}
		// エラーの文言にはラベルが入り、カレンダーの ID（メールアドレスのことがある）は入らない
		if msg := f.Error(); !strings.Contains(msg, "「"+f.Calendar.Label+"」") || strings.Contains(msg, "@example.com") {
			t.Errorf("Failures の Error() = %q", msg)
		}
	}
	if want := []string{"落ちている1", "壊れた予定", "落ちている2"}; !slices.Equal(labels, want) {
		t.Errorf("Failures のラベル = %v, want %v", labels, want)
	}
	// 取得できたカレンダーの予定は返す
	if len(got.Schedule.Events) != 1 || got.Schedule.Events[0].CalendarLabel != "正常" {
		t.Errorf("Events = %+v, want 「正常」の1件", got.Schedule.Events)
	}
	// 1つ失敗しても、ほかのカレンダーも試す
	if len(f.ids) != 4 {
		t.Errorf("リクエストしたカレンダーの数 = %d, want 4（%v）", len(f.ids), f.ids)
	}
}

func TestFetchConcurrently(t *testing.T) {
	pages := map[string][][]map[string]any{}
	var cals []domain.Calendar
	for i := range maxConcurrentFetches + 3 {
		id := fmt.Sprintf("cal%d@example.com", i)
		pages[id] = [][]map[string]any{{
			timed(fmt.Sprintf("e%d", i), fmt.Sprintf("予定%d", i), "2026-11-13T09:00:00+09:00", "2026-11-13T10:00:00+09:00"),
		}}
		cals = append(cals, domain.Calendar{ID: id, Label: fmt.Sprintf("カレンダー%d", i)})
	}
	f := &fakeCalendar{pages: pages, release: make(chan struct{})}
	s := newSource(t, f)

	type fetchReturn struct {
		result usecase.FetchResult
		err    error
	}
	done := make(chan fetchReturn, 1)
	go func() {
		r, err := s.Fetch(context.Background(), cals, testDate(t))
		done <- fetchReturn{r, err}
	}()

	// 上限の数まで、同時にリクエストが届く
	deadline := time.Now().Add(5 * time.Second)
	for f.currentInFlight() < maxConcurrentFetches {
		if time.Now().After(deadline) {
			close(f.release)
			t.Fatalf("同時に処理中のリクエストの数 = %d, want %d", f.currentInFlight(), maxConcurrentFetches)
		}
		time.Sleep(time.Millisecond)
	}
	// 上限を超えては届かない
	time.Sleep(50 * time.Millisecond)
	if n := f.currentInFlight(); n != maxConcurrentFetches {
		t.Errorf("同時に処理中のリクエストの数 = %d, want %d", n, maxConcurrentFetches)
	}
	close(f.release)

	got := <-done
	if got.err != nil || len(got.result.Failures) != 0 {
		t.Fatalf("Fetch() = %+v, %v", got.result, got.err)
	}
	// 予定は、取得が終わった順ではなく cals の順に並ぶ
	if len(got.result.Schedule.Events) != len(cals) {
		t.Fatalf("len(Events) = %d, want %d", len(got.result.Schedule.Events), len(cals))
	}
	for i, e := range got.result.Schedule.Events {
		if e.CalendarLabel != cals[i].Label {
			t.Errorf("Events[%d] のカレンダー = %q, want %q", i, e.CalendarLabel, cals[i].Label)
		}
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
