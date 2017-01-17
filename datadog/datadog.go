package datadog

import (
	"reflect"
	"time"
	"unsafe"

	"github.com/DataDog/dd-trace-go/tracer"
	"github.com/Sirupsen/logrus"
)

var log = logrus.WithField("prefix", "datadog")

const flushInterval = 2 * time.Second
const flushSpanCount = 10000

// This method extracts a reference to the default transport
// of the datadog Tracer. This is currently needed, as the default
// transport is not exported.
func extractDefaultTransport() tracer.Transport {
	tracerValue := reflect.ValueOf(tracer.DefaultTracer).Elem()
	field := tracerValue.FieldByName("transport")
	return *(*tracer.Transport)(unsafe.Pointer(field.UnsafeAddr()))
}

func submitTraces(transport tracer.Transport, spansByTrace <-chan map[uint64][]*tracer.Span) {
	for buffer := range spansByTrace {
		count := 0

		// the transport expects a list of list, where each sub-list contains only
		// spans of the same trace.
		var traces [][]*tracer.Span
		for _, spans := range buffer {
			count += len(spans)
			traces = append(traces, spans)
		}

		// if we got traces, send them!
		if len(traces) > 0 {
			log.Debugf("Sending %d spans in traces %d traces", count, len(traces))

			if _, err := transport.Send(traces); err != nil {
				log.WithError(err).Warn("Error reporting spans to datadog")
			}

			//val, _ := json.MarshalIndent(traces, "", "  ")
			//log.Info(string(val))
		}
	}
}

func sendSpansUsingTransport(transport tracer.Transport, spans <-chan *tracer.Span) {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	count := 0
	byTrace := make(map[uint64][]*tracer.Span)

	groupedSpans := make(chan map[uint64][]*tracer.Span, 2)
	defer close(groupedSpans)

	// send the spans in background
	go submitTraces(transport, groupedSpans)

	for {
		var flush bool

		select {
		case span, ok := <-spans:
			if !ok {
				log.Info("Channel closed, stopping sender")
				return
			}

			count++
			byTrace[span.TraceID] = append(byTrace[span.TraceID], span)
			flush = count >= flushSpanCount

		case <-ticker.C:
			flush = true
		}

		if flush && count > 0 {
			groupedSpans <- byTrace

			// reset collection
			count = 0
			byTrace = make(map[uint64][]*tracer.Span)
		}
	}
}

// Reports all spans written to the provided channel. This method
// blocks until the channel is closed, so better call it in a go routine.
func ReportSpans(spans <-chan *tracer.Span) {
	transport := extractDefaultTransport()
	sendSpansUsingTransport(transport, spans)
}
