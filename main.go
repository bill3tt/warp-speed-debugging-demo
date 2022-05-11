package main

import (
	"context"
	"flag"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"
)

var spanOptions = []trace.SpanStartOption{
	trace.WithSpanKind(trace.SpanKindServer),
	trace.WithAttributes([]attribute.KeyValue{
		attribute.String("environment", "demo"),
		attribute.String("job", "warp-speed-debugging"),
	}...),
}

// newStdOutExporter returns a console exporter.
func newStdOutExporter(w io.Writer) (tracesdk.SpanExporter, error) {
	return stdouttrace.New(
		stdouttrace.WithWriter(w),
		// Use human-readable output.
		stdouttrace.WithPrettyPrint(),
	)
}


func main() {
	var (
		addr          = flag.String("listen-address", ":8080", "The address to listen on for HTTP requests.")
		normDomain    = flag.Float64("normal.domain", 10, "The domain for the normal distribution.")
		normMean      = flag.Float64("normal.mean", 100, "The mean for the normal distribution.")
		logFilePath   = flag.String("log.file", "logs.txt", "The log file used to write logs to.")
		traceFilePath = flag.String("trace.logFile", "traces.txt", "The logFile used to write logs to.")
		traceEndpoint = flag.String("trace.endpoint", "", "The endpoint to send traces to.")
	)
	flag.Parse()

	logFile, err := os.Create(*logFilePath)
	if err != nil {
		log.Fatal(err)
	}
	defer logFile.Close()

	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	log.SetFormatter(&log.JSONFormatter{})

	if *traceEndpoint == "" {
		// Write telemetry data to a logFile.
		traceFile, err := os.Create(*traceFilePath)
		if err != nil {
			log.Fatal(err)
		}
		defer traceFile.Close()

		exp, err := newStdOutExporter(traceFile)
		if err != nil {
			log.Fatal(err)
		}

		tp := tracesdk.NewTracerProvider(
			tracesdk.WithBatcher(exp),
			tracesdk.WithResource(resource.Default()),
		)
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				log.Fatal(err)
			}
		}()
		otel.SetTracerProvider(tp)
	} else {
		ctx := context.Background()
		client := otlptracehttp.NewClient(
			otlptracehttp.WithEndpoint(*traceEndpoint),
			otlptracehttp.WithInsecure(),
		)
		exporter, err := otlptrace.New(ctx, client)
		if err != nil {
			log.Fatalf("creating OTLP trace exporter: %v", err)
		}

		tracerProvider := tracesdk.NewTracerProvider(
			tracesdk.WithBatcher(exporter),
			tracesdk.WithResource(resource.Default()),
		)
		otel.SetTracerProvider(tracerProvider)
	}

	var (
		// The same as above, but now as a histogram, and only for the normal
		// distribution. The buckets are targeted to the parameters of the
		// normal distribution, with 20 buckets centered on the mean, each
		// half-sigma wide.
		rpcDurationsHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "demo_rpc_durations_histogram_seconds",
			Help:    "RPC latency distributions.",
			Buckets: prometheus.LinearBuckets(*normMean-5**normDomain, .5**normDomain, 20),
		})
	)

	// Register the summary and the histogram with Prometheus's default registry.
	prometheus.MustRegister(rpcDurationsHistogram)
	// Add Go module build info.
	prometheus.MustRegister(collectors.NewBuildInfoCollector())

	go func() {
		for {
			// Each execution of the run loop, we should get a new "root" span and context.
			parentCtx, parent := otel.Tracer("demo").Start(context.Background(), "parent_span", spanOptions...)
			parent.SetAttributes(attribute.String("http.verb", "GET"))

			child1Ctx, child1 := otel.Tracer("demo").Start(parentCtx, "child_1_span", spanOptions...)
			child1.SetAttributes(attribute.String("http.verb", "GET"))

			_, grandChild1 := otel.Tracer("demo").Start(child1Ctx, "grand_child_1_span", spanOptions...)
			grandChild1.SetAttributes(attribute.String("http.verb", "GET"))
			time.Sleep(100 * time.Millisecond)
			grandChild1.End()

			time.Sleep(100 * time.Millisecond)
			child1.End()

			_, child2 := otel.Tracer("demo").Start(parentCtx, "child_2_span", spanOptions...)
			child2.SetAttributes(attribute.String("http.verb", "GET"))
			time.Sleep(100 * time.Millisecond)
			child2.End()

			traceId := parent.SpanContext().TraceID().String()
			v := (rand.NormFloat64() * *normDomain) + *normMean
			rpcDurationsHistogram.(prometheus.ExemplarObserver).ObserveWithExemplar(
				v, prometheus.Labels{"traceId": traceId},
			)

			log.WithField("traceId", traceId).Infof("Observed value %f", v)
			time.Sleep(500 * time.Millisecond)

			parent.End()
		}
	}()

	// Expose the registered metrics via HTTP.
	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))
	log.Fatal(http.ListenAndServe(*addr, nil))
}
