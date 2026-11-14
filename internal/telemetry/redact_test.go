package telemetry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	testCalendarID = "family@group.calendar.google.com"
	testHost       = "abc123.execute-api.ap-northeast-1.amazonaws.com"
)

// testProvider は、トレースを送らない Provider に、テスト用の伏せる値を足したもの。
func testProvider() *Provider {
	p := &Provider{r: &redactor{}}
	p.Hide("{calendarId}", testCalendarID)
	p.Hide("{host}", testHost)
	return p
}

func TestHideRedactsURLForms(t *testing.T) {
	p := testProvider()
	tests := []struct {
		in, want string
	}{
		{
			in:   "https://www.googleapis.com/calendar/v3/calendars/family@group.calendar.google.com/events?alt=json",
			want: "https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events?alt=json",
		},
		{
			// Google の API クライアントは、URL の中で @ を %40 にする（googleapi.Expand）
			in:   "https://www.googleapis.com/calendar/v3/calendars/family%40group.calendar.google.com/events",
			want: "https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events",
		},
		{in: "https://" + testHost + "/dev/notify", want: "https://{host}/dev/notify"},
		{in: testHost, want: "{host}"},
		{in: "関係のない文字列", want: "関係のない文字列"},
	}
	for _, tt := range tests {
		if got := p.r.redact(tt.in); got != tt.want {
			t.Errorf("redact(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRedactorReplacesLongerValuesFirst(t *testing.T) {
	r := &redactor{}
	r.add("{short}", "example.com")
	r.add("{long}", "work@example.com")
	if got := r.redact("work@example.com"); got != "{long}" {
		t.Errorf("redact() = %q, want {long}", got)
	}
}

func TestRedactorWithoutValues(t *testing.T) {
	r := &redactor{}
	r.add("{x}", "") // 空の値は無視する
	if got := r.redact(testCalendarID); got != testCalendarID {
		t.Errorf("redact() = %q, want そのまま", got)
	}
}

func TestRedactingExporter(t *testing.T) {
	p := testProvider()
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(&redactingExporter{next: mem, r: p.r}))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := tp.Tracer("test").Start(context.Background(), "GET calendars/"+testCalendarID)
	span.SetAttributes(
		attribute.String("url.full", "https://www.googleapis.com/calendar/v3/calendars/family%40group.calendar.google.com/events"),
		attribute.String("server.address", testHost),
		attribute.StringSlice("list", []string{testCalendarID, "ok"}),
		attribute.Int("count", 3),
	)
	err := errors.New(`Post "https://` + testHost + `/dev/notify": timeout`)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	span.End()

	spans := mem.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span の数 = %d, want 1", len(spans))
	}
	got := spans[0]

	// 伏せる値が、どこにも残っていない
	texts := []string{got.Name, got.Status.Description}
	for _, kv := range got.Attributes {
		texts = append(texts, kv.Value.String())
	}
	for _, ev := range got.Events {
		texts = append(texts, ev.Name)
		for _, kv := range ev.Attributes {
			texts = append(texts, kv.Value.String())
		}
	}
	for _, s := range texts {
		for _, secret := range []string{testCalendarID, "family%40group", testHost} {
			if strings.Contains(s, secret) {
				t.Errorf("伏せる値 %q が残っている: %q", secret, s)
			}
		}
	}

	attrs := map[attribute.Key]attribute.Value{}
	for _, kv := range got.Attributes {
		attrs[kv.Key] = kv.Value
	}
	if attrs["url.full"].AsString() != "https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events" {
		t.Errorf("url.full = %q", attrs["url.full"].AsString())
	}
	if attrs["count"].AsInt64() != 3 {
		t.Errorf("文字列でない属性が変わった: count = %v", attrs["count"])
	}
	if got.Status.Code != codes.Error {
		t.Errorf("status の code が変わった: %v", got.Status.Code)
	}
}

func TestSetupRedactsSpans(t *testing.T) {
	restoreOTelGlobals(t)
	srv, got := newOTLPServer(t)
	p, err := Setup(context.Background(), "test-service", envMap(map[string]string{
		envOTLPEndpoint: srv.URL + "/otlp",
		envOTLPAuth:     testOTLPAuth,
	}))
	if err != nil {
		t.Fatalf("Setup(): %v", err)
	}
	p.Hide("{calendarId}", testCalendarID)

	_, span := otel.Tracer("test").Start(context.Background(), "test")
	span.SetAttributes(attribute.String("url.full", "https://www.googleapis.com/calendar/v3/calendars/family%40group.calendar.google.com/events"))
	span.End()
	if err := p.Shutdown(); err != nil {
		t.Fatalf("Shutdown(): %v", err)
	}

	reqs := got()
	if len(reqs) != 1 {
		t.Fatalf("OTLP のリクエストの数 = %d, want 1", len(reqs))
	}
	body := string(reqs[0].body)
	if strings.Contains(body, "family%40group") || strings.Contains(body, testCalendarID) {
		t.Error("送った span にカレンダーの ID が含まれている")
	}
	if !strings.Contains(body, "{calendarId}") {
		t.Error("送った span に伏せた文字列が含まれていない")
	}
}
