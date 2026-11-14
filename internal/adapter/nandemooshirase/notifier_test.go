package nandemooshirase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/zaki3mymy/notify-schedule/internal/domain"
	"github.com/zaki3mymy/notify-schedule/internal/usecase"
)

// Notifier が usecase.Notifier を満たすこと
var _ usecase.Notifier = (*Notifier)(nil)

const testAPIKey = "dummy-api-key-for-test" //nolint:gosec // テスト用のダミーの値（本物の認証情報ではない）

// recorded は、偽サーバーが受け取ったリクエスト。
type recorded struct {
	method      string
	path        string
	apiKey      string
	contentType string
	traceparent string
	body        map[string]any
}

// newServer は、受け取ったリクエストを記録し、status と body を返す偽サーバーを作る。
func newServer(t *testing.T, status int, body string) (*httptest.Server, *[]recorded) {
	t.Helper()
	var got []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("本文を読めませんでした: %v", err)
		}
		rec := recorded{
			method:      r.Method,
			path:        r.URL.Path,
			apiKey:      r.Header.Get("x-api-key"),
			contentType: r.Header.Get("Content-Type"),
			traceparent: r.Header.Get("traceparent"),
		}
		if err := json.Unmarshal(b, &rec.body); err != nil {
			t.Errorf("本文が JSON ではありません: %v (%s)", err, b)
		}
		got = append(got, rec)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func messages(t *testing.T, texts ...string) []domain.Message {
	t.Helper()
	var msgs []domain.Message
	for _, s := range texts {
		m, err := domain.NewMessage(s)
		if err != nil {
			t.Fatalf("NewMessage(%q): %v", s, err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func newNotifier(t *testing.T, cfg Config) *Notifier {
	t.Helper()
	n, err := New(cfg)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return n
}

func TestNotifySendsMessages(t *testing.T) {
	tests := []struct {
		name       string
		dryRun     bool
		wantDryRun any // 本文の dry_run（ないときは nil）
	}{
		{name: "ふつうに送る（dry_run は付けない）", dryRun: false, wantDryRun: nil},
		{name: "dry_run を付けて送る", dryRun: true, wantDryRun: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, got := newServer(t, http.StatusOK, `{"message": "Successfully sent 2 message(s)"}`)
			n := newNotifier(t, Config{Endpoint: srv.URL + "/notify", APIKey: testAPIKey, DryRun: tt.dryRun})

			if err := n.Notify(context.Background(), messages(t, "1通目", "2通目")); err != nil {
				t.Fatalf("Notify(): %v", err)
			}

			if len(*got) != 1 {
				t.Fatalf("リクエストの数 = %d, want 1", len(*got))
			}
			r := (*got)[0]
			if r.method != http.MethodPost || r.path != "/notify" {
				t.Errorf("リクエスト = %s %s, want POST /notify", r.method, r.path)
			}
			if r.apiKey != testAPIKey {
				t.Errorf("x-api-key = %q, want %q", r.apiKey, testAPIKey)
			}
			if r.contentType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", r.contentType)
			}
			var gotMessages []string
			for _, m := range r.body["messages"].([]any) {
				gotMessages = append(gotMessages, m.(string))
			}
			if !slices.Equal(gotMessages, []string{"1通目", "2通目"}) {
				t.Errorf("messages = %q, want [1通目 2通目]", gotMessages)
			}
			if r.body["dry_run"] != tt.wantDryRun {
				t.Errorf("dry_run = %v, want %v", r.body["dry_run"], tt.wantDryRun)
			}
		})
	}
}

func TestNotifyWithoutMessagesSendsNothing(t *testing.T) {
	srv, got := newServer(t, http.StatusOK, `{}`)
	n := newNotifier(t, Config{Endpoint: srv.URL, APIKey: testAPIKey})

	if err := n.Notify(context.Background(), nil); err != nil {
		t.Fatalf("Notify(): %v", err)
	}
	if len(*got) != 0 {
		t.Errorf("メッセージがないのにリクエストが %d 回送られた", len(*got))
	}
}

func TestNotifyErrorResponses(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantMessage string
	}{
		{
			name:        "本文の error を含める",
			status:      http.StatusBadRequest,
			body:        `{"error": "No valid messages to send"}`,
			wantMessage: "No valid messages to send",
		},
		{
			name:        "JSON でない本文は、1行にして含める",
			status:      http.StatusForbidden,
			body:        "Forbidden\n  by gateway",
			wantMessage: "Forbidden by gateway",
		},
		{
			name:        "本文がない",
			status:      http.StatusInternalServerError,
			body:        "",
			wantMessage: "",
		},
		{
			name:        "長い本文は切り詰める",
			status:      http.StatusBadGateway,
			body:        strings.Repeat("x", 1000),
			wantMessage: strings.Repeat("x", 200) + "…",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := newServer(t, tt.status, tt.body)
			n := newNotifier(t, Config{Endpoint: srv.URL, APIKey: testAPIKey})

			err := n.Notify(context.Background(), messages(t, "こんにちは"))
			var statusErr *StatusError
			if !errors.As(err, &statusErr) {
				t.Fatalf("Notify() error = %v, want *StatusError", err)
			}
			if statusErr.StatusCode != tt.status || statusErr.Message != tt.wantMessage {
				t.Errorf("StatusError = {%d, %q}, want {%d, %q}", statusErr.StatusCode, statusErr.Message, tt.status, tt.wantMessage)
			}
			if !strings.Contains(err.Error(), fmt.Sprint(tt.status)) {
				t.Errorf("エラーにステータスコードが含まれていない: %q", err)
			}
			if strings.Contains(err.Error(), testAPIKey) {
				t.Errorf("エラーに API キーが含まれている: %q", err)
			}
		})
	}
}

func TestNotifyTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	n := newNotifier(t, Config{Endpoint: srv.URL, APIKey: testAPIKey, Timeout: 50 * time.Millisecond})
	err := n.Notify(context.Background(), messages(t, "こんにちは"))
	if err == nil {
		t.Fatal("Notify() succeeded, want timeout error")
	}
	var netErr interface{ Timeout() bool }
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Errorf("Notify() error = %v, want timeout", err)
	}
	if strings.Contains(err.Error(), testAPIKey) {
		t.Errorf("エラーに API キーが含まれている: %q", err)
	}
}

func TestNotifyCanceledContext(t *testing.T) {
	srv, got := newServer(t, http.StatusOK, `{}`)
	n := newNotifier(t, Config{Endpoint: srv.URL, APIKey: testAPIKey})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := n.Notify(ctx, messages(t, "こんにちは"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Notify() error = %v, want context.Canceled", err)
	}
	if len(*got) != 0 {
		t.Errorf("キャンセルされたのにリクエストが %d 回送られた", len(*got))
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{name: "エンドポイントがない", cfg: Config{APIKey: testAPIKey}, wantErr: "エンドポイント"},
		{name: "API キーがない", cfg: Config{Endpoint: "https://example.com/notify"}, wantErr: "API キー"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("New() error = %v, want to contain %q", err, tt.wantErr)
			}
		})
	}

	n := newNotifier(t, Config{Endpoint: "https://example.com/notify", APIKey: testAPIKey})
	if n.client.Timeout != DefaultTimeout {
		t.Errorf("既定のタイムアウト = %v, want %v", n.client.Timeout, DefaultTimeout)
	}
	for _, s := range []string{n.String(), fmt.Sprint(n), fmt.Sprintf("%v", n)} {
		if strings.Contains(s, testAPIKey) {
			t.Errorf("Notifier の文字列表現に API キーが含まれている: %q", s)
		}
	}
}

func TestNotifyPropagatesTraceContext(t *testing.T) {
	// 呼び出し元（cmd）と同じく、traceparent ヘッダーで引き継ぐ propagator を設定する
	prop := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prop) })

	traceID := trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		TraceFlags: trace.FlagsSampled,
	}))

	srv, got := newServer(t, http.StatusOK, `{"message": "ok"}`)
	// HTTPClient を渡さない（既定のクライアントが traceparent を付けることを確かめる）
	n := newNotifier(t, Config{Endpoint: srv.URL + "/notify", APIKey: testAPIKey})
	if err := n.Notify(ctx, messages(t, "1通目")); err != nil {
		t.Fatalf("Notify(): %v", err)
	}

	if len(*got) != 1 {
		t.Fatalf("リクエストの数 = %d, want 1", len(*got))
	}
	if tp := (*got)[0].traceparent; !strings.Contains(tp, traceID.String()) {
		t.Errorf("traceparent = %q, want トレースの ID %s を含む", tp, traceID)
	}
}

// recordSpans は、テストの間、作られた span を記録する TracerProvider をグローバルに設定する。
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	return rec
}

func TestNotifyRecordsSpans(t *testing.T) {
	t.Run("成功", func(t *testing.T) {
		rec := recordSpans(t)
		srv, _ := newServer(t, http.StatusOK, `{"message": "ok"}`)
		// HTTPClient を渡さない（既定のクライアントの HTTP の span の名前を確かめる）
		n := newNotifier(t, Config{Endpoint: srv.URL + "/notify", APIKey: testAPIKey, DryRun: true})
		if err := n.Notify(context.Background(), messages(t, "1通目", "2通目")); err != nil {
			t.Fatalf("Notify(): %v", err)
		}

		spans := rec.Ended()
		if len(spans) != 2 {
			t.Fatalf("span の数 = %d, want 2（notify と POST /notify）", len(spans))
		}
		// HTTP の span が先に終わる
		httpSpan, notifySpan := spans[0], spans[1]
		if notifySpan.Name() != "notify" {
			t.Errorf("span の名前 = %q, want notify", notifySpan.Name())
		}
		if httpSpan.Name() != "POST /notify" {
			t.Errorf("HTTP の span の名前 = %q, want POST /notify", httpSpan.Name())
		}
		if httpSpan.Parent().SpanID() != notifySpan.SpanContext().SpanID() {
			t.Error("HTTP の span の親が notify ではない")
		}
		attrs := map[attribute.Key]attribute.Value{}
		for _, kv := range notifySpan.Attributes() {
			attrs[kv.Key] = kv.Value
		}
		if attrs["notify.messages"].AsInt64() != 2 || !attrs["notify.dry_run"].AsBool() {
			t.Errorf("notify の属性 = %v, want messages=2, dry_run=true", attrs)
		}
		if notifySpan.Status().Code != codes.Unset {
			t.Errorf("status = %v, want Unset", notifySpan.Status())
		}
	})
	t.Run("失敗", func(t *testing.T) {
		rec := recordSpans(t)
		srv, _ := newServer(t, http.StatusInternalServerError, `{"error": "boom"}`)
		n := newNotifier(t, Config{Endpoint: srv.URL + "/notify", APIKey: testAPIKey})
		if err := n.Notify(context.Background(), messages(t, "1通目")); err == nil {
			t.Fatal("Notify() がエラーを返さない")
		}

		var notifySpan sdktrace.ReadOnlySpan
		for _, s := range rec.Ended() {
			if s.Name() == "notify" {
				notifySpan = s
			}
		}
		if notifySpan == nil {
			t.Fatal("notify の span がない")
		}
		if notifySpan.Status().Code != codes.Error {
			t.Errorf("status = %v, want Error", notifySpan.Status())
		}
		// エラーの本文（エンドポイントの URL を含むことがある）は記録しない
		if strings.Contains(notifySpan.Status().Description, srv.URL) || len(notifySpan.Events()) != 0 {
			t.Errorf("notify の span にエラーの本文が記録されている: %v / %v", notifySpan.Status(), notifySpan.Events())
		}
	})
	t.Run("メッセージがないときは span を作らない", func(t *testing.T) {
		rec := recordSpans(t)
		n := newNotifier(t, Config{Endpoint: "https://example.com/notify", APIKey: testAPIKey})
		if err := n.Notify(context.Background(), nil); err != nil {
			t.Fatalf("Notify(): %v", err)
		}
		if spans := rec.Ended(); len(spans) != 0 {
			t.Errorf("span の数 = %d, want 0", len(spans))
		}
	})
}
