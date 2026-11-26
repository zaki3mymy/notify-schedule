package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
)

const testOTLPAuth = "Basic dGVzdDp0b2tlbg==" //nolint:gosec // テスト用のダミーの値（本物の認証情報ではない）

// restoreOTelGlobals は、テストの終わりに、グローバルの TracerProvider・LoggerProvider・propagator を元に戻す。
func restoreOTelGlobals(t *testing.T) {
	t.Helper()
	tp, lp, prop := otel.GetTracerProvider(), otel.GetLoggerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(tp)
		otel.SetLoggerProvider(lp)
		otel.SetTextMapPropagator(prop)
	})
}

// emitLog は、グローバルの LoggerProvider に、本文が body のログを1つ送る。
func emitLog(body string, attrs ...attribute.KeyValue) {
	var r log.Record
	r.SetBody(attribute.StringValue(body))
	r.AddAttributes(attrs...)
	otel.GetLoggerProvider().Logger("test").Emit(context.Background(), r)
}

// requestByPath は、パスが path のリクエストを返す。
func requestByPath(t *testing.T, reqs []otlpRequest, path string) otlpRequest {
	t.Helper()
	var found []otlpRequest
	for _, r := range reqs {
		if r.path == path {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s へのリクエストの数 = %d, want 1", path, len(found))
	}
	return found[0]
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

func TestSetupSendsSpansAndLogs(t *testing.T) {
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

			p, err := Setup(context.Background(), "test-service", envMap(map[string]string{
				envOTLPEndpoint: tt.endpoint(srv.URL),
				envOTLPAuth:     testOTLPAuth,
			}))
			if err != nil {
				t.Fatalf("Setup(): %v", err)
			}

			_, span := otel.Tracer("test").Start(context.Background(), "test")
			span.End()
			emitLog("テストのログ")
			if err := p.Shutdown(); err != nil {
				t.Fatalf("Shutdown(): %v", err)
			}

			reqs := got()
			if len(reqs) != 2 {
				t.Fatalf("OTLP のリクエストの数 = %d, want 2（トレースとログ）", len(reqs))
			}
			for _, path := range []string{"/otlp/v1/traces", "/otlp/v1/logs"} {
				req := requestByPath(t, reqs, path)
				if req.authorization != testOTLPAuth {
					t.Errorf("%s の Authorization = %q, want %q", path, req.authorization, testOTLPAuth)
				}
				// service.name の既定値を送る
				if !strings.Contains(string(req.body), "test-service") {
					t.Errorf("%s の本文に service.name（test-service）が含まれていない", path)
				}
			}
			if !strings.Contains(string(requestByPath(t, reqs, "/otlp/v1/logs").body), "テストのログ") {
				t.Error("ログの本文が送られていない")
			}
			// 呼び出し先にトレースの ID を引き継ぐ
			if _, ok := otel.GetTextMapPropagator().(propagation.TraceContext); !ok {
				t.Errorf("propagator = %T, want propagation.TraceContext", otel.GetTextMapPropagator())
			}
		})
	}
}

func TestSetupWithoutEnvSendsNothing(t *testing.T) {
	restoreOTelGlobals(t)
	before, beforeLP := otel.GetTracerProvider(), otel.GetLoggerProvider()

	p, err := Setup(context.Background(), "test-service", envMap(nil))
	if err != nil {
		t.Fatalf("Setup(): %v", err)
	}
	// 送らないときも、Hide と Shutdown はそのまま呼べる
	p.Hide("{x}", "value")
	if err := p.Shutdown(); err != nil {
		t.Errorf("Shutdown(): %v", err)
	}
	if otel.GetTracerProvider() != before {
		t.Error("環境変数がないのに TracerProvider が変わった")
	}
	if otel.GetLoggerProvider() != beforeLP {
		t.Error("環境変数がないのに LoggerProvider が変わった")
	}
}

func TestSetupInvalidEnv(t *testing.T) {
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
			before, beforeLP := otel.GetTracerProvider(), otel.GetLoggerProvider()

			p, err := Setup(context.Background(), "test-service", envMap(tt.env))
			if err == nil {
				t.Fatal("Setup() がエラーを返さない")
			}
			// 失敗しても、呼び出し側がそのまま呼べる Provider を返す
			p.Hide("{x}", "value")
			if err := p.Shutdown(); err != nil {
				t.Errorf("Shutdown(): %v", err)
			}
			if otel.GetTracerProvider() != before {
				t.Error("失敗したのに TracerProvider が変わった")
			}
			if otel.GetLoggerProvider() != beforeLP {
				t.Error("失敗したのに LoggerProvider が変わった")
			}
		})
	}
}
