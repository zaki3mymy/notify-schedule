package googlecalendar

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/zaki3mymy/notify-schedule/internal/domain"
)

// recordSpans は、テストの間、作られた span を記録する TracerProvider をグローバルに設定する。
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	return rec
}

// spansNamed は、name の span を、終わった順に返す。
func spansNamed(rec *tracetest.SpanRecorder, name string) []sdktrace.ReadOnlySpan {
	var spans []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == name {
			spans = append(spans, s)
		}
	}
	return spans
}

func spanAttrs(s sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	m := map[attribute.Key]attribute.Value{}
	for _, kv := range s.Attributes() {
		m[kv.Key] = kv.Value
	}
	return m
}

func TestFetchRecordsSpans(t *testing.T) {
	rec := recordSpans(t)
	f := &fakeCalendar{pages: map[string][][]map[string]any{
		"work@example.com": {{
			timed("w1", "打ち合わせ", "2026-11-13T09:00:00+09:00", "2026-11-13T10:00:00+09:00"),
			timed("w2", "夕会", "2026-11-13T17:00:00+09:00", "2026-11-13T17:30:00+09:00"),
		}},
		"family@example.com": {{allDay("f1", "誕生日", "2026-11-13", "2026-11-14")}},
	}}
	s := newSource(t, f)
	cals := []domain.Calendar{
		{ID: "work@example.com", Label: "仕事"},
		{ID: "family@example.com", Label: "家族"},
	}
	if _, err := s.Fetch(context.Background(), cals, testDate(t)); err != nil {
		t.Fatalf("Fetch(): %v", err)
	}

	schedules := spansNamed(rec, "fetch_schedule")
	if len(schedules) != 1 {
		t.Fatalf("fetch_schedule の span の数 = %d, want 1", len(schedules))
	}
	parent := schedules[0]
	attrs := spanAttrs(parent)
	if attrs["schedule.calendars"].AsInt64() != 2 || attrs["schedule.events"].AsInt64() != 3 {
		t.Errorf("fetch_schedule の属性 = %v, want calendars=2, events=3", attrs)
	}

	calendars := spansNamed(rec, "fetch_calendar")
	if len(calendars) != 2 {
		t.Fatalf("fetch_calendar の span の数 = %d, want 2", len(calendars))
	}
	want := map[string]int64{"仕事": 2, "家族": 1}
	for _, c := range calendars {
		a := spanAttrs(c)
		label := a["calendar.label"].AsString()
		if n, ok := want[label]; !ok || a["calendar.events"].AsInt64() != n {
			t.Errorf("fetch_calendar の属性 = %v", a)
		}
		if c.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Errorf("fetch_calendar（%s）の親が fetch_schedule ではない", label)
		}
	}
}

func TestFetchErrorSpansDoNotContainCalendarID(t *testing.T) {
	rec := recordSpans(t)
	f := &fakeCalendar{failing: map[string]bool{"down@example.com": true}}
	s := newSource(t, f)
	cals := []domain.Calendar{{ID: "down@example.com", Label: "落ちている"}}
	if _, err := s.Fetch(context.Background(), cals, testDate(t)); err == nil {
		t.Fatal("Fetch() がエラーを返さない")
	}

	for _, name := range []string{"fetch_schedule", "fetch_calendar"} {
		spans := spansNamed(rec, name)
		if len(spans) != 1 {
			t.Fatalf("%s の span の数 = %d, want 1", name, len(spans))
		}
		sp := spans[0]
		if sp.Status().Code != codes.Error {
			t.Errorf("%s の status = %v, want Error", name, sp.Status())
		}
		// カレンダーの ID（メールアドレスのことがある）を span に出さない
		texts := []string{sp.Status().Description}
		for _, kv := range sp.Attributes() {
			texts = append(texts, kv.Value.String())
		}
		for _, ev := range sp.Events() {
			texts = append(texts, ev.Name)
			for _, kv := range ev.Attributes {
				texts = append(texts, kv.Value.String())
			}
		}
		for _, s := range texts {
			if strings.Contains(s, "down@example.com") {
				t.Errorf("%s の span にカレンダーの ID が含まれている: %q", name, s)
			}
		}
	}
}
