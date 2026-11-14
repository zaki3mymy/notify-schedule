package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const testOTLPAuth = "Basic dGVzdDp0b2tlbg==" //nolint:gosec // テスト用のダミーの値（本物の認証情報ではない）

// restoreOTelGlobals は、テストの終わりに、グローバルの TracerProvider と propagator を元に戻す。
func restoreOTelGlobals(t *testing.T) {
	t.Helper()
	tp, prop := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(prop)
	})
}

// envMap は、map から読む LookupEnv。
func envMap(m map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

// otlpRequest は、偽の OTLP の受け口が受け取ったリクエスト。
type otlpRequest struct {
	path          string
	authorization string
	body          []byte
}

// newOTLPServer は、受け取ったリクエストを記録する偽の OTLP の受け口を作る。
func newOTLPServer(t *testing.T) (*httptest.Server, func() []otlpRequest) {
	t.Helper()
	var (
		mu  sync.Mutex
		got []otlpRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, otlpRequest{path: r.URL.Path, authorization: r.Header.Get("Authorization"), body: b})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []otlpRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]otlpRequest(nil), got...)
	}
}

func TestSetupTracingSendsSpans(t *testing.T) {
	tests := []struct {
		name     string
		endpoint func(srvURL string) string
	}{
		{name: "末尾の / なし", endpoint: func(u string) string { return u + "/otlp" }},
		{name: "末尾の / あり", endpoint: func(u string) string { return u + "/otlp/" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreOTelGlobals(t)
			srv, got := newOTLPServer(t)

			shutdown, err := setupTracing(context.Background(), envMap(map[string]string{
				envOTLPEndpoint: tt.endpoint(srv.URL),
				envOTLPAuth:     testOTLPAuth,
			}), nil)
			if err != nil {
				t.Fatalf("setupTracing(): %v", err)
			}

			_, span := otel.Tracer(tracerName).Start(context.Background(), "test")
			span.End()
			if err := shutdown(context.Background()); err != nil {
				t.Fatalf("shutdown(): %v", err)
			}

			reqs := got()
			if len(reqs) != 1 {
				t.Fatalf("OTLP のリクエストの数 = %d, want 1", len(reqs))
			}
			if reqs[0].path != "/otlp/v1/traces" {
				t.Errorf("送信先のパス = %q, want /otlp/v1/traces", reqs[0].path)
			}
			if reqs[0].authorization != testOTLPAuth {
				t.Errorf("Authorization = %q, want %q", reqs[0].authorization, testOTLPAuth)
			}
			if len(reqs[0].body) == 0 {
				t.Error("本文が空")
			}
			// 呼び出し先にトレースの ID を引き継ぐ
			if _, ok := otel.GetTextMapPropagator().(propagation.TraceContext); !ok {
				t.Errorf("propagator = %T, want propagation.TraceContext", otel.GetTextMapPropagator())
			}
		})
	}
}

func TestSetupTracingWithoutEnvSendsNothing(t *testing.T) {
	restoreOTelGlobals(t)
	before := otel.GetTracerProvider()

	shutdown, err := setupTracing(context.Background(), envMap(nil), nil)
	if err != nil {
		t.Fatalf("setupTracing(): %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown(): %v", err)
	}
	if otel.GetTracerProvider() != before {
		t.Error("環境変数がないのに TracerProvider が変わった")
	}
}

func TestSetupTracingInvalidEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "送信先だけ", env: map[string]string{envOTLPEndpoint: "https://example.com/otlp"}},
		{name: "認証情報だけ", env: map[string]string{envOTLPAuth: testOTLPAuth}},
		{name: "送信先が空", env: map[string]string{envOTLPEndpoint: "", envOTLPAuth: testOTLPAuth}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreOTelGlobals(t)
			before := otel.GetTracerProvider()

			shutdown, err := setupTracing(context.Background(), envMap(tt.env), nil)
			if err == nil {
				t.Fatal("setupTracing() がエラーを返さない")
			}
			// 失敗しても、呼び出し側がそのまま呼べる shutdown を返す
			if err := shutdown(context.Background()); err != nil {
				t.Errorf("shutdown(): %v", err)
			}
			if otel.GetTracerProvider() != before {
				t.Error("失敗したのに TracerProvider が変わった")
			}
		})
	}
}

// recordSpans は、テストの間、作られた span を記録する TracerProvider をグローバルに設定する。
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	restoreOTelGlobals(t)
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	return rec
}
