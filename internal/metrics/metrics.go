package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	Registry *prometheus.Registry

	CallAttempts     *prometheus.CounterVec
	CallTerminal     *prometheus.CounterVec
	CallsActive      prometheus.Gauge
	CallSetupSeconds prometheus.Histogram

	SignalingMessages *prometheus.CounterVec
	SignalingErrors   *prometheus.CounterVec
	SignalingDropped  prometheus.Counter
	ActiveSockets     prometheus.Gauge

	ReconnectAttempts *prometheus.CounterVec

	DirectConnections prometheus.Counter
	RelayConnections  prometheus.Counter
	ICEFailures       prometheus.Counter
	ICERestarts       prometheus.Counter

	MediaRTT         prometheus.Histogram
	MediaJitter      prometheus.Histogram
	MediaPacketsLost prometheus.Counter

	TURNCredentials prometheus.Counter
	TURNErrors      prometheus.Counter
	TURNRelays      prometheus.Counter

	HTTPRequests *prometheus.CounterVec
}

func New() *Metrics {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &Metrics{
		Registry: registry,
		CallAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "worm_call_attempts_total",
			Help: "Call creation attempts by outcome.",
		}, []string{"outcome"}),
		CallTerminal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "worm_call_terminal_total",
			Help: "Calls entering a terminal state.",
		}, []string{"state"}),
		CallsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "worm_calls_active",
			Help: "Calls currently not in a terminal state.",
		}),
		CallSetupSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "worm_call_setup_seconds",
			Help:    "Time from call creation to connected state.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 3, 5, 8, 13, 21, 30, 60},
		}),
		SignalingMessages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "worm_signaling_messages_total",
			Help: "Signaling messages accepted by type.",
		}, []string{"type"}),
		SignalingErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "worm_signaling_errors_total",
			Help: "Signaling errors returned to clients by code.",
		}, []string{"code"}),
		SignalingDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worm_signaling_dropped_total",
			Help: "Signaling messages dropped because the peer queue was full.",
		}),
		ActiveSockets: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "worm_websockets_active",
			Help: "Authenticated signaling WebSocket connections.",
		}),
		ReconnectAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "worm_reconnect_attempts_total",
			Help: "Signaling reconnect attempts by outcome.",
		}, []string{"outcome"}),
		DirectConnections: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worm_media_direct_total",
			Help: "Calls whose selected candidate pair was not relayed.",
		}),
		RelayConnections: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worm_media_relay_total",
			Help: "Calls whose selected candidate pair used TURN relay.",
		}),
		ICEFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worm_ice_failures_total",
			Help: "Peer connections that reached the failed ICE state.",
		}),
		ICERestarts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worm_ice_restarts_total",
			Help: "ICE restarts performed by clients.",
		}),
		MediaRTT: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "worm_media_rtt_seconds",
			Help:    "Reported RTT of the selected candidate pair.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.2, 0.4, 0.8, 1.6},
		}),
		MediaJitter: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "worm_media_jitter_seconds",
			Help:    "Reported inbound jitter.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2},
		}),
		MediaPacketsLost: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worm_media_packets_lost_total",
			Help: "Reported inbound packets lost.",
		}),
		TURNCredentials: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worm_turn_credentials_total",
			Help: "TURN credentials issued.",
		}),
		TURNErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worm_turn_errors_total",
			Help: "TURN credential or allocation errors.",
		}),
		TURNRelays: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "worm_turn_relays_total",
			Help: "TURN allocations created.",
		}),
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "worm_http_requests_total",
			Help: "HTTP requests by method and status class.",
		}, []string{"method", "status"}),
	}

	registry.MustRegister(
		m.CallAttempts,
		m.CallTerminal,
		m.CallsActive,
		m.CallSetupSeconds,
		m.SignalingMessages,
		m.SignalingErrors,
		m.SignalingDropped,
		m.ActiveSockets,
		m.ReconnectAttempts,
		m.DirectConnections,
		m.RelayConnections,
		m.ICEFailures,
		m.ICERestarts,
		m.MediaRTT,
		m.MediaJitter,
		m.MediaPacketsLost,
		m.TURNCredentials,
		m.TURNErrors,
		m.TURNRelays,
		m.HTTPRequests,
	)

	m.initSeries()
	return m
}

func (m *Metrics) initSeries() {
	for _, outcome := range []string{"created", "accepted", "rejected", "cancelled", "rate_limited", "callee_unavailable", "error"} {
		m.CallAttempts.WithLabelValues(outcome)
	}
	for _, state := range []string{"ended", "rejected", "cancelled", "timeout", "failed"} {
		m.CallTerminal.WithLabelValues(state)
	}
	for _, typ := range []string{
		"call.create", "call.incoming", "call.accept", "call.reject", "call.cancel",
		"call.end", "call.state", "call.error", "webrtc.offer", "webrtc.answer",
		"webrtc.ice_candidate", "webrtc.ice_candidate_complete", "webrtc.renegotiate",
		"connection.recover", "connection.status", "ping", "pong",
	} {
		m.SignalingMessages.WithLabelValues(typ)
	}
	for _, code := range []string{
		"invalid_message", "invalid_state", "forbidden", "not_found", "call_expired",
		"participant_unavailable", "rate_limited", "message_too_large", "internal_error",
		"unauthorized", "already_in_call", "bad_request", "request_conflict",
	} {
		m.SignalingErrors.WithLabelValues(code)
	}
	for _, outcome := range []string{"ok", "failed"} {
		m.ReconnectAttempts.WithLabelValues(outcome)
	}
	for _, class := range []string{"2xx", "3xx", "4xx", "5xx"} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			m.HTTPRequests.WithLabelValues(method, class)
		}
	}
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveCallCreated() {
	m.CallAttempts.WithLabelValues("created").Inc()
	m.CallsActive.Inc()
}

func (m *Metrics) ObserveCallRejected(outcome string) {
	m.CallAttempts.WithLabelValues(outcome).Inc()
}

func (m *Metrics) ObserveTerminal(state string, setupSeconds float64) {
	m.CallTerminal.WithLabelValues(state).Inc()
	m.CallsActive.Dec()
	if setupSeconds > 0 {
		m.CallSetupSeconds.Observe(setupSeconds)
	}
}
