package main

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/zaki3mymy/notify-schedule/internal/config"
)

// トレースの送信先と認証情報を読む環境変数。
// Collector を挟まず、Grafana Cloud の OTLP のエンドポイントに直接送る。
const (
	// envOTLPEndpoint は、OTLP の送信先（例: https://otlp-gateway-prod-ap-northeast-0.grafana.net/otlp）。
	// この後ろに /v1/traces を付けて送る。
	envOTLPEndpoint = "GRAFANA_CLOUD_OTLP_ENDPOINT"
	// envOTLPAuth は、Authorization ヘッダーの値（Basic <base64(スタックの ID:トークン)>）。
	// SDK 標準の OTEL_EXPORTER_OTLP_HEADERS は "Authorization=..." の形を求めるが、Secret Manager には
	// ヘッダーの値だけが入っていて、Cloud Run の環境変数では前に文字を付け足せないので、自分で読む。
	envOTLPAuth = "GRAFANA_CLOUD_OTLP_AUTH"
)

const (
	// serviceName は、service.name の既定値（OTEL_SERVICE_NAME で上書きできる）。
	serviceName = "notify-schedule"
	// tracerName は、このアプリが作る span の計装の名前。
	tracerName = "github.com/zaki3mymy/notify-schedule"
)

// setupTracing は、トレースを OTLP で送る TracerProvider を用意し、グローバルに設定する。
// 返す関数を終了の前に呼ぶと、溜まっている span を送り切る（Cloud Run Jobs はそれを待ってから終わる）。
//
// 送信先と認証情報の環境変数がどちらもないとき（手元やテスト）は、何も送らない。
// 片方だけのときは、設定の誤りなのでエラーにする。
// service.name などのリソースの属性は、環境変数 OTEL_SERVICE_NAME・OTEL_RESOURCE_ATTRIBUTES から読む。
func setupTracing(ctx context.Context, lookupEnv config.LookupEnv) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }

	endpoint, hasEndpoint := lookupEnv(envOTLPEndpoint)
	auth, hasAuth := lookupEnv(envOTLPAuth)
	if !hasEndpoint && !hasAuth {
		return noop, nil
	}
	if endpoint == "" || auth == "" {
		return noop, fmt.Errorf("トレースを送るには %s と %s の両方が必要です", envOTLPEndpoint, envOTLPAuth)
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(strings.TrimSuffix(endpoint, "/")+"/v1/traces"),
		otlptracehttp.WithHeaders(map[string]string{"Authorization": auth}),
	)
	if err != nil {
		return noop, fmt.Errorf("トレースの exporter を作れませんでした: %w", err)
	}

	// service.name の既定値を入れ、環境変数（OTEL_SERVICE_NAME・OTEL_RESOURCE_ATTRIBUTES）があれば上書きする
	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", serviceName)),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return noop, fmt.Errorf("トレースのリソースを作れませんでした: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	// 呼び出し先（nandemo-oshirase）にトレースの ID を引き継ぐ（traceparent ヘッダー）
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp.Shutdown, nil
}
