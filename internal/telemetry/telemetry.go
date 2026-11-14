// Package telemetry は、トレースを Grafana Cloud に OTLP で送る仕組み（送り先・認証・送る前に値を伏せる処理）。
//
// Collector は挟まず、アプリから直接送る（理由は docs/architecture.md の「トレースの送り方」を参照）。
// cmd からは、Setup で用意し、Hide で伏せる値を足し、終了の前に Shutdown を呼ぶだけでよい。
//
// このパッケージは、設定（config）やドメインに依存しない。何を伏せるかは呼び出し側（cmd）が決める。
package telemetry

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// トレースの送信先と認証情報を読む環境変数。
const (
	// envOTLPEndpoint は、OTLP の送信先（例: https://otlp-gateway-prod-ap-northeast-0.grafana.net/otlp）。
	// この後ろに /v1/traces を付けて送る。
	envOTLPEndpoint = "GRAFANA_CLOUD_OTLP_ENDPOINT"
	// envOTLPAuth は、Authorization ヘッダーの値（Basic <base64(スタックの ID:トークン)>）。
	// SDK 標準の OTEL_EXPORTER_OTLP_HEADERS は "Authorization=..." の形を求めるが、Secret Manager には
	// ヘッダーの値だけが入っていて、Cloud Run の環境変数では前に文字を付け足せないので、自分で読む。
	envOTLPAuth = "GRAFANA_CLOUD_OTLP_AUTH"
)

// shutdownTimeout は、Shutdown で、溜まっている span を送り切るのを待つ時間の上限。
const shutdownTimeout = 10 * time.Second

// LookupEnv は、環境変数を読む関数（os.LookupEnv と同じ形。テストで差し替える）。
type LookupEnv func(key string) (string, bool)

// Provider は、Setup で用意したトレースの仕組み。
// トレースを送らないとき（環境変数がない・準備に失敗した）も、Hide と Shutdown はそのまま呼べる。
type Provider struct {
	tp *sdktrace.TracerProvider // nil なら、トレースを送らない
	r  *redactor
}

// Setup は、トレースを OTLP で送る TracerProvider を用意し、グローバルに設定する。
//
// 送信先と認証情報の環境変数がどちらもないとき（手元やテスト）は、何も送らない。
// 片方だけのときは、設定の誤りなのでエラーにする（そのときも、何も送らない Provider を返す）。
// serviceName は service.name の既定値で、環境変数 OTEL_SERVICE_NAME があればそちらを使う。
// ほかのリソースの属性は、環境変数 OTEL_RESOURCE_ATTRIBUTES から読む。
func Setup(ctx context.Context, serviceName string, lookupEnv LookupEnv) (*Provider, error) {
	p := &Provider{r: &redactor{}}

	endpoint, hasEndpoint := lookupEnv(envOTLPEndpoint)
	auth, hasAuth := lookupEnv(envOTLPAuth)
	if !hasEndpoint && !hasAuth {
		return p, nil
	}
	if endpoint == "" || auth == "" {
		return p, fmt.Errorf("トレースを送るには %s と %s の両方が必要です", envOTLPEndpoint, envOTLPAuth)
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(strings.TrimSuffix(endpoint, "/")+"/v1/traces"),
		otlptracehttp.WithHeaders(map[string]string{"Authorization": auth}),
	)
	if err != nil {
		return p, fmt.Errorf("トレースの exporter を作れませんでした: %w", err)
	}

	// service.name の既定値を入れ、環境変数（OTEL_SERVICE_NAME・OTEL_RESOURCE_ATTRIBUTES）があれば上書きする
	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", serviceName)),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return p, fmt.Errorf("トレースのリソースを作れませんでした: %w", err)
	}

	p.tp = sdktrace.NewTracerProvider(
		// 送る直前に、伏せる値を置き換える（redact.go）
		sdktrace.WithBatcher(&redactingExporter{next: exporter, r: p.r}),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(p.tp)
	// 呼び出し先（nandemo-oshirase）にトレースの ID を引き継ぐ（traceparent ヘッダー）
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return p, nil
}

// Hide は、トレースに出したくない値を、placeholder に置き換えて送るようにする。空の値は無視する。
//
// 値は、URL のパスやクエリに入ると形が変わる（例: メールアドレスの @ が %40 になる）ので、その形も伏せる。
// span を作る前（外部の API を呼ぶ前）に呼ぶ。
func (p *Provider) Hide(placeholder string, values ...string) {
	for _, v := range values {
		p.r.add(placeholder, v, url.PathEscape(v), url.QueryEscape(v))
	}
}

// Shutdown は、溜まっている span を送り切る（最大10秒待つ）。終了の前に1回呼ぶ。
// 呼び出し側の context が終わっていても（SIGTERM を受け取ったあとでも）送れるよう、新しい context を使う。
func (p *Provider) Shutdown() error {
	if p.tp == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return p.tp.Shutdown(ctx)
}
