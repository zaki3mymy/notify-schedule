package main

import (
	"slices"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/zaki3mymy/notify-schedule/internal/config"
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

// fakeHider は、Hide に渡された値を、代わりの文字列ごとに記録する。
type fakeHider map[string][]string

func (h fakeHider) Hide(placeholder string, values ...string) {
	h[placeholder] = append(h[placeholder], values...)
}

func TestHideConfigInTraces(t *testing.T) {
	h := fakeHider{}
	hideConfigInTraces(h, config.Config{
		Calendars: []domain.Calendar{
			{ID: "work@example.com", Label: "仕事"},
			{ID: "family@group.calendar.google.com", Label: "家族"},
		},
		NandemoOshirase: config.NandemoOshirase{
			Endpoint: "https://abc123.execute-api.ap-northeast-1.amazonaws.com/dev/notify",
			APIKey:   config.Secret(testAPIKey),
		},
	})

	if got := h[redactedCalendarID]; !slices.Equal(got, []string{"work@example.com", "family@group.calendar.google.com"}) {
		t.Errorf("カレンダーの ID として伏せる値 = %q", got)
	}
	if got := h[redactedNandemo]; !slices.Contains(got, "abc123.execute-api.ap-northeast-1.amazonaws.com") {
		t.Errorf("nandemo-oshirase として伏せる値 = %q, want エンドポイントのホストを含む", got)
	}
	if got := h[redactedSecret]; !slices.Equal(got, []string{testAPIKey}) {
		t.Errorf("API キーとして伏せる値 = %q", got)
	}
}
