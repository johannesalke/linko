## What's all this then?

This is my repo for Boot.dev's Logging and Observability course.
For this course students are given an already functional product (an url shortening service), and have to iteratively build a logging & observation infrastructure for that product. The first half primarily focuses on the logging tools available in Go, while the latter primarily uses universal tools like Prometheus, Graphana and OpenTelemetry.



#### Logging
- Creating loggers to console or file in Go
- Multiloggers that distinguish based on event level (Debug, Info, Error, etc)
- Structured logging with key-value pairs


#### Observability

- How to integrate Prometheus with a containerized application to retrieve data on its performance and metrics.
- Connecting Prometheus to Graphana for dashboard visualization and allerts.
- Profiling the performance of the go program.
- Setting up tracers & spans with Opentelementry/Jaeger to inspect which parts of the program represent the greatest time costs

