package main

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/zaki3mymy/notify-schedule/internal/config"
)

// トレースで、伏せた値の代わりに入れる文字列。
const (
	redactedCalendarID = "{calendarId}"
	redactedNandemo    = "{nandemo-oshirase}"
	redactedSecret     = "{redacted}"
)

// redactor は、トレースに出したくない値（カレンダーの ID、nandemo-oshirase のエンドポイントなど）を、
// 代わりの文字列に置き換える。
//
// HTTP の span の url.full・server.address や、エラーの本文（URL を含むことがある）には、これらの値が
// そのまま入る。どこに入るかを1つずつ追うのではなく、送る直前に、span の文字列をまとめて置き換える。
// 伏せる値は設定を読んだあとにわかるので、あとから足せるようにしている。
type redactor struct {
	mu       sync.RWMutex
	pairs    map[string]string // 伏せる値 → 代わりの文字列
	replacer *strings.Replacer // nil なら、伏せる値がない
}

// add は、values を placeholder に置き換えるようにする。空の値は無視する。
func (r *redactor) add(placeholder string, values ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pairs == nil {
		r.pairs = map[string]string{}
	}
	for _, v := range values {
		if v != "" {
			r.pairs[v] = placeholder
		}
	}
	// 長い値から先に置き換える（短い値が長い値の一部のときに、長い値が残らないように）
	keys := make([]string, 0, len(r.pairs))
	for k := range r.pairs {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	oldnew := make([]string, 0, len(keys)*2)
	for _, k := range keys {
		oldnew = append(oldnew, k, r.pairs[k])
	}
	r.replacer = strings.NewReplacer(oldnew...)
}

// redact は、s の中の伏せる値を置き換える。
func (r *redactor) redact(s string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.replacer == nil {
		return s
	}
	return r.replacer.Replace(s)
}

// addConfig は、設定のうち、トレースに出したくない値を伏せるようにする。
//
//   - カレンダーの ID（メールアドレスのことがある）。URL のパスやクエリに入るときの形も含める
//   - nandemo-oshirase のエンドポイントのホスト（API Gateway の URL を公開したくないため）
//   - nandemo-oshirase の API キー（span には入らないはずだが、念のため）
func (r *redactor) addConfig(cfg config.Config) {
	for _, cal := range cfg.Calendars {
		r.add(redactedCalendarID,
			cal.ID,
			url.PathEscape(cal.ID),
			url.QueryEscape(cal.ID),
			strings.ReplaceAll(cal.ID, "@", "%40"),
		)
	}
	if u, err := url.Parse(cfg.NandemoOshirase.Endpoint); err == nil {
		r.add(redactedNandemo, u.Host, u.Hostname())
	}
	r.add(redactedSecret, cfg.NandemoOshirase.APIKey.Value())
}

// redactingExporter は、span の文字列（名前・属性・イベント・status）の伏せる値を置き換えてから、
// next に渡す SpanExporter。
type redactingExporter struct {
	next sdktrace.SpanExporter
	r    *redactor
}

func (e *redactingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	out := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		out[i] = redactedSpan{ReadOnlySpan: s, r: e.r}
	}
	return e.next.ExportSpans(ctx, out)
}

func (e *redactingExporter) Shutdown(ctx context.Context) error {
	return e.next.Shutdown(ctx)
}

// redactedSpan は、文字列を置き換えて見せる ReadOnlySpan。
type redactedSpan struct {
	sdktrace.ReadOnlySpan
	r *redactor
}

func (s redactedSpan) Name() string { return s.r.redact(s.ReadOnlySpan.Name()) }

func (s redactedSpan) Attributes() []attribute.KeyValue {
	return s.r.redactAttributes(s.ReadOnlySpan.Attributes())
}

func (s redactedSpan) Events() []sdktrace.Event {
	events := s.ReadOnlySpan.Events()
	out := make([]sdktrace.Event, len(events))
	for i, ev := range events {
		ev.Name = s.r.redact(ev.Name)
		ev.Attributes = s.r.redactAttributes(ev.Attributes)
		out[i] = ev
	}
	return out
}

func (s redactedSpan) Status() sdktrace.Status {
	st := s.ReadOnlySpan.Status()
	st.Description = s.r.redact(st.Description)
	return st
}

// redactAttributes は、文字列（と文字列の配列）の属性の値を置き換える。
func (r *redactor) redactAttributes(attrs []attribute.KeyValue) []attribute.KeyValue {
	out := make([]attribute.KeyValue, len(attrs))
	for i, kv := range attrs {
		switch kv.Value.Type() {
		case attribute.STRING:
			out[i] = attribute.String(string(kv.Key), r.redact(kv.Value.AsString()))
		case attribute.STRINGSLICE:
			vs := kv.Value.AsStringSlice()
			for j := range vs {
				vs[j] = r.redact(vs[j])
			}
			out[i] = attribute.StringSlice(string(kv.Key), vs)
		default:
			out[i] = kv
		}
	}
	return out
}
