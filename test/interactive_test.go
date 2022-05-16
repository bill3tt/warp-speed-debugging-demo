package test

import (
	"fmt"
	"github.com/efficientgo/e2e"
	e2einteractive "github.com/efficientgo/e2e/interactive"
	"github.com/efficientgo/tools/core/pkg/testutil"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const (
	logsDir = "logs"
)

func TestInteractiveExemplars(t *testing.T) {
	// Start isolated environment with given ref.
	e, err := e2e.NewDockerEnvironment("exemplar_example")
	testutil.Ok(t, err)
	// Make sure resources (e.g docker containers, network, dir) are cleaned.
	t.Cleanup(e.Close)

	// Setup Logs & Traces
	loki := NewLoki(e, "loki")
	tempo := NewTempo(e, "tempo")
	err = e2e.StartAndWaitReady(loki, tempo)
	testutil.Ok(t, err)

	// Setup Application
	demo, internalLogFilePath := NewDemo(e, "demo", tempo.InternalEndpoint("oltp-http"))
	err = e2e.StartAndWaitReady(demo)
	testutil.Ok(t, err)

	// Setup Promtail
	tail := NewPromtail(e, "promtail", loki.InternalEndpoint("http"), internalLogFilePath)
	err = e2e.StartAndWaitReady(tail)
	testutil.Ok(t, err)

	// Setup Metrics
	prom := NewPrometheus(e, "prometheus", demo.InternalEndpoint("http"))
	err = e2e.StartAndWaitReady(prom)
	testutil.Ok(t, err)

	// Setup & start Grafana
	grafana := NewGrafana(e, "grafana",
		"http://"+prom.InternalEndpoint("http"),
		"http://"+loki.InternalEndpoint("http"),
		"http://"+tempo.InternalEndpoint("http"),
	)
	err = e2e.StartAndWaitReady(grafana)
	testutil.Ok(t, err)

	err = e2einteractive.OpenInBrowser("http://" + grafana.Endpoint("http"))
	testutil.Ok(t, err)

	// Wait for user input before exiting
	err = e2einteractive.RunUntilEndpointHit()
}

func NewPromtail(env e2e.Environment, name string, lokiURL string, internalLogsPath string) e2e.InstrumentedRunnable {
	ports := map[string]int{"http": 9080}

	promtail := e2e.NewInstrumentedRunnable(env, name).WithPorts(ports, "http").Future()

	config := fmt.Sprintf(`
server:
  http_listen_address: 0.0.0.0
  http_listen_port: %d

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://%s/loki/api/v1/push#
    batchwait: 1s

scrape_configs:

- job_name: demo
  static_configs:
  - targets:
      - localhost
    labels:
      job: demo
      __path__: %s
  pipeline_stages:
  - json:
      expressions:
        traceId: traceId
        level: level
  - labels:
      traceId:
      level:
`, ports["http"], lokiURL, internalLogsPath)

	configFileName := "config.yaml"
	configPath := filepath.Join(promtail.Dir(), configFileName)
	internalConfigPath := filepath.Join(promtail.InternalDir(), configFileName)

	if err := ioutil.WriteFile(configPath, []byte(config), 0600); err != nil {
		return e2e.NewErrInstrumentedRunnable(name, errors.Wrap(err, "create prometheus config failed"))
	}

	args := map[string]string {
		"-config.file": internalConfigPath,
	}

	return promtail.Init(e2e.StartOptions{
		Image:   "grafana/promtail:2.5.0",
		User:    strconv.Itoa(os.Getuid()),
		Command: e2e.NewCommandWithoutEntrypoint("promtail", e2e.BuildArgs(args)...),
	})

}

func NewDemo(env e2e.Environment, name string, tempoUrl string) (e2e.InstrumentedRunnable, string) {
	ports := map[string]int{"http": 8080}

	demo := e2e.NewInstrumentedRunnable(env, name).WithPorts(ports, "http").Future()

	if err := os.MkdirAll(filepath.Join(demo.Dir(), logsDir), os.ModePerm); err != nil {
		return e2e.NewErrInstrumentedRunnable(name, errors.Wrap(err, "create logs dir failed")), ""
	}

	internalLogsPath := filepath.Join(demo.InternalDir(), logsDir, "logs.txt")

	fmt.Println(tempoUrl)

	args := map[string]string{
		"-log.file":       internalLogsPath,
		"-trace.endpoint": tempoUrl,
	}

	return demo.Init(e2e.StartOptions{
		Image:     "warp-speed-debugging:latest",
		User:      strconv.Itoa(os.Getuid()),
		Command:   e2e.NewCommandWithoutEntrypoint("warp-speed-debugging-demo", e2e.BuildArgs(args)...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/metrics", 200, 200),
	}), internalLogsPath
}

func NewPrometheus(env e2e.Environment, name string, demoApplicationUrl string) e2e.InstrumentedRunnable {

	ports := map[string]int{"http": 9090}

	f := e2e.NewInstrumentedRunnable(env, name).WithPorts(ports, "http").Future()
	config := fmt.Sprintf(`
global:
  external_labels:
    prometheus: %v
scrape_configs:
- job_name: 'myself'
  # Quick scrapes for test purposes.
  scrape_interval: 1s
  scrape_timeout: 1s
  static_configs:
  - targets: [%s]
  relabel_configs:
  - source_labels: ['__address__']
    regex: '^.+:80$'
    action: drop
- job_name: 'application'
  scrape_interval: 1s
  scrape_timeout: 1s
  static_configs:
  - targets: [%s]
  relabel_configs:
  - source_labels: ['__address__']
    regex: '^.+:80$'
    action: drop
`, name, f.InternalEndpoint("http"), demoApplicationUrl)
	if err := ioutil.WriteFile(filepath.Join(f.Dir(), "prometheus.yml"), []byte(config), 0600); err != nil {
		return e2e.NewErrInstrumentedRunnable(name, errors.Wrap(err, "create prometheus config failed"))
	}

	args := map[string]string{
		"--config.file":                     filepath.Join(f.InternalDir(), "prometheus.yml"),
		"--storage.tsdb.path":               f.InternalDir(),
		"--storage.tsdb.max-block-duration": "2h", // No compaction - mostly not needed for quick test.
		"--log.level":                       "info",
		"--web.listen-address":              fmt.Sprintf(":%d", ports["http"]),
		"--enable-feature":                  "exemplar-storage",
	}

	return f.Init(e2e.StartOptions{
		Image:     "quay.io/prometheus/prometheus:v2.35.0",
		Command:   e2e.NewCommandWithoutEntrypoint("prometheus", e2e.BuildArgs(args)...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	})
}

func NewGrafana(env e2e.Environment, name string, promUrl string, lokiUrl string, tempoUrl string) e2e.InstrumentedRunnable {

	ports := map[string]int{"http": 3000}

	f := e2e.NewInstrumentedRunnable(env, name).WithPorts(ports, "http").Future()

	// DO NOT USE this configuration file in any non-example setting.
	// It disabled authentication and gives anonymous users admin access to this Grafana instance.
	config := fmt.Sprintf(`
[auth.anonymous]
enabled = true
org_name = Main Org.
org_role = Admin

[security]
cookie_samesite = none

[feature_toggles]
enable = tempoSearch tempoBackendSearch
`)
	if err := ioutil.WriteFile(filepath.Join(f.Dir(), "grafana.ini"), []byte(config), 0600); err != nil {
		return e2e.NewErrInstrumentedRunnable(name, errors.Wrap(err, "create grafana config failed"))
	}

	datasources := fmt.Sprintf(`
apiVersion: 1
datasources:
  - name: Prometheus
    uid: prometheus
    url: %s
    type: prometheus
    jsonData:
      httpMethod: POST
      exemplarTraceIdDestinations:
        - datasourceUid: tempo
          name: traceId
  - name: Tempo
    uid: tempo
    url: %s
    type: tempo`, promUrl, tempoUrl)
	if err := os.MkdirAll(filepath.Join(f.Dir(), "datasources"), os.ModePerm); err != nil {
		return e2e.NewErrInstrumentedRunnable(name, errors.Wrap(err, "create grafana datasources dir failed"))
	}
	if err := ioutil.WriteFile(filepath.Join(f.Dir(), "datasources", "datasources.yaml"), []byte(datasources), os.ModePerm); err != nil {
		return e2e.NewErrInstrumentedRunnable(name, errors.Wrap(err, "create grafana datasources failed"))
	}

	return f.Init(e2e.StartOptions{
		Image: "grafana/grafana:8.3.2",
		User:  strconv.Itoa(os.Getuid()),
		EnvVars: map[string]string{
			"GF_PATHS_CONFIG":       filepath.Join(f.InternalDir(), "grafana.ini"),
			"GF_PATHS_PROVISIONING": f.InternalDir(),
		},
	})
}

func NewLoki(env e2e.Environment, name string) e2e.InstrumentedRunnable {
	ports := map[string]int{
		"http": 3100,
	}

	f := e2e.NewInstrumentedRunnable(env, name).WithPorts(ports, "http").Future()

	config := `
auth_enabled: false

server:
  http_listen_port: 3100

ingester:
  lifecycler:
    address: 127.0.0.1
    ring:
      kvstore:
        store: inmemory
      replication_factor: 1
    final_sleep: 0s
  chunk_idle_period: 5m
  chunk_retain_period: 30s

schema_config:
  configs:
  - from: 2018-04-15
    store: boltdb
    object_store: filesystem
    schema: v9
    index:
      prefix: index_
      period: 168h

storage_config:
  boltdb:
    directory: /tmp/loki/index

  filesystem:
    directory: /tmp/loki/chunks

limits_config:
  enforce_metric_name: false
  reject_old_samples: true
  reject_old_samples_max_age: 168h

chunk_store_config:
  max_look_back_period: 0

table_manager:
  chunk_tables_provisioning:
    inactive_read_throughput: 0
    inactive_write_throughput: 0
    provisioned_read_throughput: 0
    provisioned_write_throughput: 0
  index_tables_provisioning:
    inactive_read_throughput: 0
    inactive_write_throughput: 0
    provisioned_read_throughput: 0
    provisioned_write_throughput: 0
  retention_deletes_enabled: false
  retention_period: 0
`

	if err := ioutil.WriteFile(filepath.Join(f.Dir(), "loki.yaml"), []byte(config), os.ModePerm); err != nil {
		return e2e.NewErrInstrumentedRunnable(name, errors.Wrap(err, "create loki config failed"))
	}

	args := e2e.BuildArgs(map[string]string{
		"-config.file":      filepath.Join(f.InternalDir(), "loki.yaml"),
		"-ingester.wal-dir": f.InternalDir(),
	})

	return f.Init(
		e2e.StartOptions{
			Image:   "grafana/loki:2.5.0",
			User:    strconv.Itoa(os.Getuid()),
			Command: e2e.NewCommandWithoutEntrypoint("loki", args...),
			Volumes: []string{f.Dir()},
		},
	)
}

func NewTempo(env e2e.Environment, name string) e2e.InstrumentedRunnable {
	config := `
server:
  http_listen_port: 3200

distributor:
  receivers:                           # this configuration will listen on all ports and protocols that tempo is capable of.
    jaeger:                            # the receives all come from the OpenTelemetry collector.  more configuration information can
      protocols:                       # be found there: https://github.com/open-telemetry/opentelemetry-collector/tree/main/receiver
        thrift_http:                   #
        grpc:                          # for a production deployment you should only enable the receivers you need!
        thrift_binary:
        thrift_compact:
    zipkin:
    otlp:
      protocols:
        http:
        grpc:
    opencensus:

ingester:
  trace_idle_period: 10s               # the length of time after a trace has not received spans to consider it complete and flush it
  max_block_bytes: 1_000_000           # cut the head block when it hits this size or ...
  max_block_duration: 5m               #   this much time passes

compactor:
  compaction:
    compaction_window: 1h              # blocks in this time window will be compacted together
    max_block_bytes: 100_000_000       # maximum size of compacted blocks
    block_retention: 1h
    compacted_block_retention: 10m

storage:
  trace:
    backend: local                     # backend configuration to use
    block:
      bloom_filter_false_positive: .05 # bloom filter false positive rate.  lower values create larger filters but fewer false positives
      index_downsample_bytes: 1000     # number of bytes per index record
      encoding: zstd                   # block encoding/compression.  options: none, gzip, lz4-64k, lz4-256k, lz4-1M, lz4, snappy, zstd, s2
    wal:
      path: /tmp/tempo/wal             # where to store the the wal locally
      encoding: snappy                 # wal encoding/compression.  options: none, gzip, lz4-64k, lz4-256k, lz4-1M, lz4, snappy, zstd, s2
    local:
      path: /tmp/tempo/blocks
    pool:
      max_workers: 100                 # worker pool determines the number of parallel requests to the object store backend
      queue_depth: 10000

search_enabled: true
`
	ports := map[string]int{
		"http":      3200,
		"jaeger":    14268,
		"oltp-grpc": 4317,
		"oltp-http": 4318,
		"zipkin":    9411,
	}

	f := e2e.NewInstrumentedRunnable(env, name).WithPorts(ports, "http").Future()

	if err := ioutil.WriteFile(filepath.Join(f.Dir(), "tempo.yaml"), []byte(config), os.ModePerm); err != nil {
		return e2e.NewErrInstrumentedRunnable(name, errors.Wrap(err, "create tempo config failed"))
	}

	args := e2e.BuildArgs(map[string]string{
		"-config.file": filepath.Join(f.InternalDir(), "tempo.yaml"),
	})

	return f.Init(
		e2e.StartOptions{
			Image:   "grafana/tempo:1.4.1",
			User:    strconv.Itoa(os.Getuid()),
			Command: e2e.NewCommandWithoutEntrypoint("/tempo", args...),
			Volumes: []string{f.Dir()},
		},
	)
}
