module github.com/bill3tt/warp-speed-debugging-demo

go 1.16

require (
	github.com/efficientgo/e2e v0.12.1
	github.com/efficientgo/tools/core v0.0.0-20220225185207-fe763185946b
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.12.1
	github.com/sirupsen/logrus v1.6.0
	go.opentelemetry.io/otel v1.7.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.7.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.7.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.7.0
	go.opentelemetry.io/otel/sdk v1.7.0
	go.opentelemetry.io/otel/trace v1.7.0
)
