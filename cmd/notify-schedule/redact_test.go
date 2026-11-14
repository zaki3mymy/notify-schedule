package main

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

	"github.com/zaki3mymy/notify-schedule/internal/config"
	"github.com/zaki3mymy/notify-schedule/internal/domain"
)

func testRedactConfig() config.Config {
	return config.Config{
		Calendars: []domain.Calendar{
			{ID: "work@example.com", Label: "仕事"},
			{ID: "family@group.calendar.google.com", Label: "家族"},
		},
		NandemoOshirase: config.NandemoOshirase{
			Endpoint: "https://abc123.execute-api.ap-northeast-1.amazonaws.com/dev/notify",
			APIKey:   config.Secret(testAPIKey),
		},
	}
}

func TestRedactorRedact(t *testing.T) {
	r := &redactor{}
	r.addConfig(testRedactConfig())

	tests := []struct {
		in, want string
	}{
		{
			in:   "https://www.googleapis.com/calendar/v3/calendars/work@example.com/events?alt=json",
			want: "https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events?alt=json",
		},
		{
			in:   "https://www.googleapis.com/calendar/v3/calendars/work%40example.com/events",
			want: "https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events",
		},
		{
			in:   "https://abc123.execute-api.ap-northeast-1.amazonaws.com/dev/notify",
			want: "https://{nandemo-oshirase}/dev/notify",
		},
		{in: "abc123.execute-api.ap-northeast-1.amazonaws.com", want: "{nandemo-oshirase}"},
		{in: "key=" + testAPIKey, want: "key={redacted}"},
		{in: "関係のない文字列", want: "関係のない文字列"},
	}
	for _, tt := range tests {
		if got := r.redact(tt.in); got != tt.want {
			t.Errorf("redact(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRedactorWithoutValues(t *testing.T) {
	r := &redactor{}
	r.add(redactedSecret, "") // 空の値は無視する
	if got := r.redact("work@example.com"); got != "work@example.com" {
		t.Errorf("redact() = %q, want そのまま", got)
	}
}

func TestRedactingExporter(t *testing.T) {
	r := &redactor{}
	r.addConfig(testRedactConfig())
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(&redactingExporter{next: mem, r: r}))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := tp.Tracer("test").Start(context.Background(), "GET calendars/work@example.com")
	span.SetAttributes(
		attribute.String("url.full", "https://www.googleapis.com/calendar/v3/calendars/work@example.com/events"),
		attribute.String("server.address", "abc123.execute-api.ap-northeast-1.amazonaws.com"),
		attribute.StringSlice("list", []string{"family@group.calendar.google.com", "ok"}),
		attribute.Int("count", 3),
	)
	err := errors.New(`Post "https://abc123.execute-api.ap-northeast-1.amazonaws.com/dev/notify": timeout`)
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
		for _, secret := range []string{"work@example.com", "family@group.calendar.google.com", "abc123.execute-api"} {
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

func TestSetupTracingRedactsSpans(t *testing.T) {
	restoreOTelGlobals(t)
	srv, got := newOTLPServer(t)
	r := &redactor{}
	shutdown, err := setupTracing(context.Background(), envMap(map[string]string{
		envOTLPEndpoint: srv.URL + "/otlp",
		envOTLPAuth:     testOTLPAuth,
	}), r)
	if err != nil {
		t.Fatalf("setupTracing(): %v", err)
	}
	r.addConfig(testRedactConfig())

	_, span := otel.Tracer(tracerName).Start(context.Background(), "test")
	span.SetAttributes(attribute.String("url.full", "https://www.googleapis.com/calendar/v3/calendars/work@example.com/events"))
	span.End()
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown(): %v", err)
	}

	reqs := got()
	if len(reqs) != 1 {
		t.Fatalf("OTLP のリクエストの数 = %d, want 1", len(reqs))
	}
	if strings.Contains(string(reqs[0].body), "work@example.com") {
		t.Error("送った span にカレンダーの ID が含まれている")
	}
	if !strings.Contains(string(reqs[0].body), redactedCalendarID) {
		t.Error("送った span に伏せた文字列が含まれていない")
	}
}
