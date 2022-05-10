package main

import (
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"math/rand"
	"net/http"
	"time"
)

func main() {
	var (
		addr              = flag.String("listen-address", ":8080", "The address to listen on for HTTP requests.")
		normDomain        = flag.Float64("normal.domain", 10, "The domain for the normal distribution.")
		normMean          = flag.Float64("normal.mean", 100, "The mean for the normal distribution.")
	)

	flag.Parse()

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
			v := (rand.NormFloat64() * *normDomain) + *normMean
			rpcDurationsHistogram.(prometheus.ExemplarObserver).ObserveWithExemplar(
				v, prometheus.Labels{"traceID": fmt.Sprint(rand.Intn(100000))},
			)
			time.Sleep(100 * time.Millisecond)
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
