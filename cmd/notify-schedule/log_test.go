package main

import (
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log/logtest"
	"go.opentelemetry.io/otel/trace"
)

// recordLogs は、テストの間、OpenTelemetry に送られたログを記録する LoggerProvider をグローバルに設定する。
func recordLogs(t *testing.T) *logtest.Recorder {
	t.Helper()
	prev := otel.GetLoggerProvider()
	t.Cleanup(func() { otel.SetLoggerProvider(prev) })
	rec := logtest.NewRecorder()
	otel.SetLoggerProvider(rec)
	return rec
}

// otelLogs は、記録したログを、本文ごとに返す。
func otelLogs(t *testing.T, rec *logtest.Recorder) map[string]logtest.Record {
	t.Helper()
	got := map[string]logtest.Record{}
	for scope, records := range rec.Result() {
		if scope.Name != instrumentationName {
			t.Errorf("計装の名前 = %q, want %q", scope.Name, instrumentationName)
		}
		for _, r := range records {
			got[r.Body.AsString()] = r
		}
	}
	return got
}

func TestRunSendsLogsToOTel(t *testing.T) {
	spans := recordSpans(t)
	rec := recordLogs(t)
	h := newHarness(t, validConfig)
	h.source.events = testEvents()
	if code := h.run(); code != exitOK {
		t.Fatalf("run() = %d, want %d\n%s", code, exitOK, h.stdout.String())
	}

	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("span の数 = %d, want 1", len(ended))
	}
	logs := otelLogs(t, rec)
	// 標準出力（Cloud Logging）と同じログを送る
	if len(logs) != len(h.logs()) {
		t.Errorf("OpenTelemetry に送ったログの数 = %d, want 標準出力と同じ", len(logs))
	}

	got, ok := logs["通知しました"]
	if !ok {
		t.Fatalf("「通知しました」のログがない: %v", logs)
	}
	if got.SeverityText != "INFO" {
		t.Errorf("SeverityText = %q, want INFO", got.SeverityText)
	}
	// span の中で出したログは、その span につながる（trace_id・span_id が付く）
	sc := trace.SpanContextFromContext(got.Context)
	if sc.TraceID() != ended[0].SpanContext().TraceID() || sc.SpanID() != ended[0].SpanContext().SpanID() {
		t.Errorf("ログの span = %v, want %v", sc, ended[0].SpanContext())
	}
}
