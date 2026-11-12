package usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zaki3mymy/notify-schedule/internal/domain"
)

// fakeSource は、決まった予定かエラーを返す ScheduleSource。呼ばれた引数を記録する。
type fakeSource struct {
	events []domain.Event
	err    error
	// onFetch は、Fetch の中で呼ぶ（キャンセルの再現に使う）
	onFetch func()

	calls   int
	gotCals []domain.Calendar
	gotDate domain.Date
}

func (s *fakeSource) Fetch(_ context.Context, cals []domain.Calendar, d domain.Date) (domain.DailySchedule, error) {
	s.calls++
	s.gotCals = cals
	s.gotDate = d
	if s.onFetch != nil {
		s.onFetch()
	}
	if s.err != nil {
		return domain.DailySchedule{}, s.err
	}
	return domain.DailySchedule{Date: d, Events: s.events}, nil
}

// fakeNotifier は、受け取ったメッセージを記録する Notifier。
type fakeNotifier struct {
	err error

	calls int
	got   []domain.Message
}

func (n *fakeNotifier) Notify(_ context.Context, msgs []domain.Message) error {
	n.calls++
	n.got = msgs
	return n.err
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

func testEvents(d domain.Date) []domain.Event {
	start := d.Start().Add(9 * time.Hour)
	return []domain.Event{
		{Title: "打ち合わせ", Start: start, End: start.Add(time.Hour), CalendarLabel: "仕事"},
		{Title: "誕生日", Start: d.Start(), End: d.End(), AllDay: true, CalendarLabel: "家族"},
	}
}

var testCalendars = []domain.Calendar{
	{ID: "work@example.com", Label: "仕事"},
	{ID: "family@example.com", Label: "家族"},
}

func TestRunNotifiesComposedMessagesOnce(t *testing.T) {
	d := testDate(t)
	source := &fakeSource{events: testEvents(d)}
	notifier := &fakeNotifier{}

	result, err := NewNotifySchedule(source, notifier).Run(context.Background(), testCalendars, d)
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}

	// 取得元には、指定したカレンダーと日付がそのまま渡る
	if source.calls != 1 || !slices.Equal(source.gotCals, testCalendars) || source.gotDate != d {
		t.Errorf("Fetch の呼び出し = %d 回 (%v, %v), want 1 回 (%v, %v)", source.calls, source.gotCals, source.gotDate, testCalendars, d)
	}

	// 通知は1回だけで、ドメインで変換したメッセージがそのまま渡る
	want := domain.ComposeMessages(domain.DailySchedule{Date: d, Events: testEvents(d)})
	if notifier.calls != 1 {
		t.Fatalf("Notify の呼び出し = %d 回, want 1 回", notifier.calls)
	}
	if !slices.Equal(notifier.got, want) {
		t.Errorf("Notify に渡ったメッセージ = %v, want %v", notifier.got, want)
	}

	if result != (Result{Events: 2, Messages: len(want)}) || !result.Notified() {
		t.Errorf("Run() = %+v (Notified=%v), want Events=2, Messages=%d, Notified=true", result, result.Notified(), len(want))
	}
}

func TestRunDoesNotNotifyWhenNoEvents(t *testing.T) {
	d := testDate(t)
	source := &fakeSource{events: nil}
	notifier := &fakeNotifier{}

	result, err := NewNotifySchedule(source, notifier).Run(context.Background(), testCalendars, d)
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if notifier.calls != 0 {
		t.Errorf("予定がないのに Notify が %d 回呼ばれた", notifier.calls)
	}
	if result != (Result{}) || result.Notified() {
		t.Errorf("Run() = %+v (Notified=%v), want ゼロ値", result, result.Notified())
	}
}

func TestRunFailures(t *testing.T) {
	d := testDate(t)
	errFetch := errors.New("取得元のエラー")
	errNotify := errors.New("通知先のエラー")

	tests := []struct {
		name         string
		cals         []domain.Calendar
		source       *fakeSource
		notifier     *fakeNotifier
		wantErr      error
		wantErrText  string
		wantFetches  int
		wantNotifies int
		wantResult   Result
	}{
		{
			name:        "カレンダーが指定されていない",
			cals:        nil,
			source:      &fakeSource{events: testEvents(d)},
			notifier:    &fakeNotifier{},
			wantErrText: "カレンダーが指定されていません",
		},
		{
			name:        "取得に失敗したら通知しない",
			cals:        testCalendars,
			source:      &fakeSource{err: errFetch},
			notifier:    &fakeNotifier{},
			wantErr:     errFetch,
			wantErrText: "2026-11-13 の予定の取得に失敗しました",
			wantFetches: 1,
		},
		{
			name:         "通知に失敗したらエラーを返す",
			cals:         testCalendars,
			source:       &fakeSource{events: testEvents(d)},
			notifier:     &fakeNotifier{err: errNotify},
			wantErr:      errNotify,
			wantErrText:  "2026-11-13 の予定の通知に失敗しました",
			wantFetches:  1,
			wantNotifies: 1,
			wantResult:   Result{Events: 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewNotifySchedule(tt.source, tt.notifier).Run(context.Background(), tt.cals, d)
			if err == nil {
				t.Fatal("Run() succeeded, want error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("Run() error = %v, want errors.Is(%v)", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErrText) {
				t.Errorf("Run() error = %q, want to contain %q", err, tt.wantErrText)
			}
			if tt.source.calls != tt.wantFetches || tt.notifier.calls != tt.wantNotifies {
				t.Errorf("Fetch = %d 回, Notify = %d 回, want %d 回, %d 回", tt.source.calls, tt.notifier.calls, tt.wantFetches, tt.wantNotifies)
			}
			if result != tt.wantResult || result.Notified() {
				t.Errorf("Run() = %+v, want %+v（Notified=false）", result, tt.wantResult)
			}
		})
	}
}

func TestRunDoesNotNotifyAfterCancel(t *testing.T) {
	d := testDate(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 取得の途中でキャンセルされた（取得そのものは成功した）
	source := &fakeSource{events: testEvents(d), onFetch: cancel}
	notifier := &fakeNotifier{}

	_, err := NewNotifySchedule(source, notifier).Run(ctx, testCalendars, d)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
	if notifier.calls != 0 {
		t.Errorf("キャンセルされたのに Notify が %d 回呼ばれた", notifier.calls)
	}
}
