# Warp Speed Debugging with Prometheus Exemplars - Demo

This repo contains a full end-to-end example of how to use Prometheus exemplars to take your debugging to warp-speed!

## Quick Start

To get started, simply run:

```
make test-interactive
```

## Components

This repo has two key components:
* `main.go` - Simple demo application that calculates random fibonacci numbers, and emits traces, logs and metrics! (with exemplars of course)
* `TestInteractiveExemplars` - Test suite that spins up all of the relevant components & configuration in order to see exemplars in action: Prometheus, Loki, Tempo, Grafana etc.
