package observability

import (
	"context"
	"time"

	"github.com/alexey-va/my-utils-api/internal/wireguard"
	"github.com/prometheus/client_golang/prometheus"
)

const wireGuardCollectionTimeout = 2 * time.Second

type WireGuardRelaySource interface {
	ListRelays(context.Context) ([]wireguard.Relay, error)
}

type wireGuardCollector struct {
	source            WireGuardRelaySource
	collectionSuccess *prometheus.Desc
	relayReady        *prometheus.Desc
	routingHealthy    *prometheus.Desc
	agentLastSeen     *prometheus.Desc
	exitHealthy       *prometheus.Desc
	exitSelected      *prometheus.Desc
	exitPreference    *prometheus.Desc
	exitLatency       *prometheus.Desc
	routeLoss         *prometheus.Desc
	routeRTT          *prometheus.Desc
}

func newWireGuardCollector(source WireGuardRelaySource) *wireGuardCollector {
	labels := []string{"relay_id", "relay"}
	return &wireGuardCollector{
		source: source,
		collectionSuccess: prometheus.NewDesc(
			"myutils_wireguard_collection_success",
			"Whether the persisted WireGuard relay state was collected successfully.", nil, nil,
		),
		relayReady: prometheus.NewDesc(
			"myutils_wireguard_relay_ready", "Whether the relay data plane is READY.", labels, nil,
		),
		routingHealthy: prometheus.NewDesc(
			"myutils_wireguard_routing_healthy", "Whether relay policy routing and private DNS are healthy.", labels, nil,
		),
		agentLastSeen: prometheus.NewDesc(
			"myutils_wireguard_agent_last_seen_timestamp_seconds", "Unix timestamp of the last relay agent heartbeat.", labels, nil,
		),
		exitHealthy: prometheus.NewDesc(
			"myutils_wireguard_exit_healthy", "Whether an AWG exit passed its handshake and expected-egress probe.", append(labels, "exit"), nil,
		),
		exitSelected: prometheus.NewDesc(
			"myutils_wireguard_exit_selected", "Whether an AWG exit is currently selected for external client traffic.", append(labels, "exit"), nil,
		),
		exitPreference: prometheus.NewDesc(
			"myutils_wireguard_exit_preference", "One-hot configured AWG exit preference.", append(labels, "preference"), nil,
		),
		exitLatency: prometheus.NewDesc(
			"myutils_wireguard_exit_latency_seconds", "Measured ICMP latency through an AWG exit.", append(labels, "exit"), nil,
		),
		routeLoss: prometheus.NewDesc(
			"myutils_wireguard_route_packet_loss_percent", "Packet loss measured for an internal or external route probe.", append(labels, "path"), nil,
		),
		routeRTT: prometheus.NewDesc(
			"myutils_wireguard_route_rtt_seconds", "Average RTT measured for an internal or external route probe.", append(labels, "path"), nil,
		),
	}
}

func (collector *wireGuardCollector) Describe(channel chan<- *prometheus.Desc) {
	for _, description := range []*prometheus.Desc{
		collector.collectionSuccess, collector.relayReady, collector.routingHealthy,
		collector.agentLastSeen, collector.exitHealthy, collector.exitSelected,
		collector.exitPreference, collector.exitLatency, collector.routeLoss, collector.routeRTT,
	} {
		channel <- description
	}
}

func (collector *wireGuardCollector) Collect(channel chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), wireGuardCollectionTimeout)
	defer cancel()
	relays, err := collector.source.ListRelays(ctx)
	if err != nil {
		channel <- prometheus.MustNewConstMetric(collector.collectionSuccess, prometheus.GaugeValue, 0)
		return
	}
	channel <- prometheus.MustNewConstMetric(collector.collectionSuccess, prometheus.GaugeValue, 1)
	for _, relay := range relays {
		labels := []string{relay.ID, relay.Name}
		channel <- prometheus.MustNewConstMetric(collector.relayReady, prometheus.GaugeValue, boolean(relay.Status == "READY"), labels...)
		channel <- prometheus.MustNewConstMetric(collector.routingHealthy, prometheus.GaugeValue, boolean(relay.RoutingHealthy != nil && *relay.RoutingHealthy), labels...)
		lastSeen := float64(0)
		if relay.LastSeenAt != nil {
			lastSeen = float64(relay.LastSeenAt.Unix())
		}
		channel <- prometheus.MustNewConstMetric(collector.agentLastSeen, prometheus.GaugeValue, lastSeen, labels...)
		for _, exit := range []string{"primary", "secondary"} {
			probe, exists := wireguard.ExitProbeHealth{}, false
			if relay.ExitHealth != nil {
				probe, exists = relay.ExitHealth.Exits[exit]
			}
			exitLabels := append(labels, exit)
			channel <- prometheus.MustNewConstMetric(collector.exitHealthy, prometheus.GaugeValue, boolean(exists && probe.Healthy), exitLabels...)
			selected := relay.ExitHealth != nil && relay.ExitHealth.ActiveExit != nil && *relay.ExitHealth.ActiveExit == exit
			channel <- prometheus.MustNewConstMetric(collector.exitSelected, prometheus.GaugeValue, boolean(selected), exitLabels...)
			if exists && probe.LatencyMs != nil {
				channel <- prometheus.MustNewConstMetric(collector.exitLatency, prometheus.GaugeValue, *probe.LatencyMs/1000, exitLabels...)
			}
		}
		for _, preference := range []string{"AUTO", "PRIMARY", "SECONDARY"} {
			preferenceLabels := append(labels, preference)
			channel <- prometheus.MustNewConstMetric(collector.exitPreference, prometheus.GaugeValue, boolean(relay.ExitPreference == preference), preferenceLabels...)
		}
		if relay.RouteQuality != nil {
			collector.collectRoute(channel, labels, "internal", relay.RouteQuality.Direct)
			collector.collectRoute(channel, labels, "external", relay.RouteQuality.Veesp)
		}
	}
}

func (collector *wireGuardCollector) collectRoute(channel chan<- prometheus.Metric, labels []string, path string, probe wireguard.RouteProbe) {
	pathLabels := append(labels, path)
	channel <- prometheus.MustNewConstMetric(collector.routeLoss, prometheus.GaugeValue, probe.PacketLossPercent, pathLabels...)
	if probe.AverageRTTMs != nil {
		channel <- prometheus.MustNewConstMetric(collector.routeRTT, prometheus.GaugeValue, *probe.AverageRTTMs/1000, pathLabels...)
	}
}

func boolean(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
