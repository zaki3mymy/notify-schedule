package telemetry

import (
	"context"
	"slices"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// redactor は、トレースとログに出したくない値（カレンダーの ID、エンドポイントのホストなど）を、
// 代わりの文字列に置き換える。
//
// HTTP の span の url.full・server.address や、エラーの本文（URL を含むことがある）には、これらの値が
// そのまま入る。どこに入るかを1つずつ追うのではなく、送る直前に、span の文字列をまとめて置き換える。
// 伏せる値は呼び出し側が設定を読んだあとにわかるので、あとから足せるようにしている（Provider.Hide）。
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

// redactAttributes は、属性の値の文字列を置き換える。
func (r *redactor) redactAttributes(attrs []attribute.KeyValue) []attribute.KeyValue {
	out := make([]attribute.KeyValue, len(attrs))
	for i, kv := range attrs {
		out[i] = attribute.KeyValue{Key: kv.Key, Value: r.redactValue(kv.Value)}
	}
	return out
}

// redactValue は、値の中の文字列を置き換える。配列とマップ（ログの属性で使う）は、中の値もたどる。
func (r *redactor) redactValue(v attribute.Value) attribute.Value {
	switch v.Type() {
	case attribute.STRING:
		return attribute.StringValue(r.redact(v.AsString()))
	case attribute.STRINGSLICE:
		vs := v.AsStringSlice()
		for i := range vs {
			vs[i] = r.redact(vs[i])
		}
		return attribute.StringSliceValue(vs)
	case attribute.SLICE:
		vs := v.AsSlice()
		out := make([]attribute.Value, len(vs))
		for i, e := range vs {
			out[i] = r.redactValue(e)
		}
		return attribute.SliceValue(out...)
	case attribute.MAP:
		kvs := v.AsMap()
		out := make([]attribute.KeyValue, len(kvs))
		for i, kv := range kvs {
			out[i] = attribute.KeyValue{Key: attribute.Key(r.redact(string(kv.Key))), Value: r.redactValue(kv.Value)}
		}
		return attribute.MapValue(out...)
	default:
		return v
	}
}

// redactingProcessor は、ログの本文と属性の伏せる値を置き換えてから、next に渡す Processor。
//
// slog のログには、エラーの文字列（URL を含むことがある）やカレンダーの ID がそのまま入るので、
// トレースと同じく、送る直前にまとめて置き換える。
type redactingProcessor struct {
	next sdklog.Processor
	r    *redactor
}

func (p *redactingProcessor) Enabled(ctx context.Context, param sdklog.EnabledParameters) bool {
	return p.next.Enabled(ctx, param)
}

func (p *redactingProcessor) OnEmit(ctx context.Context, record *sdklog.Record) error {
	record.SetBody(p.r.redactValue(record.Body()))
	attrs := make([]attribute.KeyValue, 0, record.AttributesLen())
	record.WalkAttributes(func(kv attribute.KeyValue) bool {
		attrs = append(attrs, attribute.KeyValue{Key: kv.Key, Value: p.r.redactValue(kv.Value)})
		return true
	})
	record.SetAttributes(attrs...)
	return p.next.OnEmit(ctx, record)
}

func (p *redactingProcessor) Shutdown(ctx context.Context) error { return p.next.Shutdown(ctx) }

func (p *redactingProcessor) ForceFlush(ctx context.Context) error { return p.next.ForceFlush(ctx) }
