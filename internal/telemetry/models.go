// Package telemetry defines the metadata-only observability contract shared by services and collectors.
package telemetry

import "strings"

// ApplicationMeasurement represents aggregate application path quality for existing performance dashboards.
// Application is an approved product identifier rather than a user-supplied destination or traffic payload.
// Baseline and optimized values allow the dashboard to compare a selected path with direct internet.
// Validate rejects malformed values before a writer constructs InfluxDB line protocol.
type ApplicationMeasurement struct {
	Application        string  `json:"application"`
	BaselineLatencyMS  float64 `json:"baselineLatencyMs"`
	OptimizedLatencyMS float64 `json:"optimizedLatencyMs"`
	BaselineLossPct    float64 `json:"baselineLossPct"`
	OptimizedLossPct   float64 `json:"optimizedLossPct"`
	BandwidthBytes     int64   `json:"bandwidthBytes"`
}

// EndpointTelemetry records aggregate VPN and network quality reported by a managed endpoint.
// It intentionally excludes packet payloads, hostnames, DNS names, URLs, and application content.
// Counters are interface-level totals that Grafana can convert into rates without flow inspection.
// Validate protects the metrics store from unsafe tags and impossible quality values.
type EndpointTelemetry struct {
	DeviceID      string  `json:"deviceId"`
	NetworkType   string  `json:"networkType"`
	VPNProvider   string  `json:"vpnProvider"`
	VPNConnected  bool    `json:"vpnConnected"`
	LatencyMS     float64 `json:"latencyMs"`
	JitterMS      float64 `json:"jitterMs"`
	LossPct       float64 `json:"lossPct"`
	ReceiveBytes  int64   `json:"receiveBytes"`
	TransmitBytes int64   `json:"transmitBytes"`
}

// EndpointDiagnostics records optional local connectivity and resource indicators.
// Pointer probes distinguish an unconfigured approved probe from a configured probe that failed.
// Interface metadata is deliberately coarse and excludes SSID, BSSID, gateway, carrier, and route details.
// Validate constrains all values before they become time-series tags or fields.
type EndpointDiagnostics struct {
	DeviceID              string  `json:"deviceId"`
	NetworkType           string  `json:"networkType"`
	ActiveInterface       string  `json:"activeInterface"`
	VPNProvider           string  `json:"vpnProvider"`
	VPNConnected          bool    `json:"vpnConnected"`
	InterfaceChanged      bool    `json:"interfaceChanged"`
	DefaultRouteAvailable bool    `json:"defaultRouteAvailable"`
	DNSReachable          *bool   `json:"dnsReachable,omitempty"`
	TLSReachable          *bool   `json:"tlsReachable,omitempty"`
	CPULoad1              float64 `json:"cpuLoad1"`
	MemoryAvailableBytes  int64   `json:"memoryAvailableBytes"`
	WiFiSignalDBM         *int    `json:"wifiSignalDbm,omitempty"`
}

// ApplicationExperience records a supported application SLO before and after a routing decision.
// DecisionID correlates this aggregate outcome with a route explanation while remaining an InfluxDB field.
// SLO names are allowlisted to keep dashboard dimensions stable and product semantics explicit.
// The record contains quality aggregates only, not packet data or application content.
type ApplicationExperience struct {
	DeviceID        string  `json:"deviceId"`
	Application     string  `json:"application"`
	SLO             string  `json:"slo"`
	BaselineValue   float64 `json:"baselineValue"`
	OptimizedValue  float64 `json:"optimizedValue"`
	TargetValue     float64 `json:"targetValue"`
	AvoidedIncident bool    `json:"avoidedIncident"`
	DecisionID      string  `json:"decisionId"`
}

// CandidatePath captures one candidate path considered during a route decision.
// Its quality values come from approved endpoint or gateway probes rather than synthetic mutation.
// Selected identifies the one candidate that won the decision comparison.
// Validate is invoked by RouteDecision to ensure the candidate is safe to persist.
type CandidatePath struct {
	Name      string  `json:"name"`
	LatencyMS float64 `json:"latencyMs"`
	JitterMS  float64 `json:"jitterMs"`
	LossPct   float64 `json:"lossPct"`
	Score     int     `json:"score"`
	Selected  bool    `json:"selected"`
}

// RouteDecision records why a policy selected, retained, or rolled back a candidate path.
// DestinationClass is an approved category and never a hostname, URL, or raw network destination.
// CandidatePaths captures the before-and-after comparison needed to explain an outcome to operators.
// Persisting this record observes decisions only; it never instructs a host to change traffic routing.
type RouteDecision struct {
	DecisionID       string          `json:"decisionId"`
	DeviceID         string          `json:"deviceId"`
	Application      string          `json:"application"`
	DestinationClass string          `json:"destinationClass"`
	PolicyID         string          `json:"policyId"`
	Reason           string          `json:"reason"`
	Result           string          `json:"result"`
	RollbackEvent    string          `json:"rollbackEvent"`
	CandidatePaths   []CandidatePath `json:"candidatePaths"`
}

// Validate checks whether application measurements can be encoded safely in InfluxDB.
// It requires a safe non-empty application identifier and non-negative quality and bandwidth values.
// The method is intentionally independent of HTTP so non-web producers can use the same contract.
// A false result means the caller must not submit the measurement to persistent telemetry storage.
func (m ApplicationMeasurement) Validate() bool {
	return SafeTag(m.Application) && m.BaselineLatencyMS >= 0 && m.OptimizedLatencyMS >= 0 && m.BaselineLossPct >= 0 && m.OptimizedLossPct >= 0 && m.BandwidthBytes >= 0
}

// Validate verifies identifiers, quality metrics, and counters for one endpoint telemetry sample.
// It requires safe device, network, and VPN-provider tags to protect InfluxDB line-protocol formatting.
// Loss is bounded as a percentage while latency, jitter, and interface counters cannot be negative.
// VPN state is boolean and therefore requires no separate range validation.
func (m EndpointTelemetry) Validate() bool {
	return SafeTag(m.DeviceID) && SafeTag(m.NetworkType) && SafeTag(m.VPNProvider) && m.LatencyMS >= 0 && m.JitterMS >= 0 && m.LossPct >= 0 && m.LossPct <= 100 && m.ReceiveBytes >= 0 && m.TransmitBytes >= 0
}

// Validate verifies the metadata-only endpoint diagnostics contract before persistence.
// It requires coarse interface and network identifiers but never accepts raw route or Wi-Fi identity data.
// Optional reachability results remain valid when the administrator has not configured a probe target.
// Wi-Fi signal, when available, is constrained to plausible negative dBm values.
func (m EndpointDiagnostics) Validate() bool {
	return SafeTag(m.DeviceID) && SafeTag(m.NetworkType) && SafeTag(m.ActiveInterface) && SafeTag(m.VPNProvider) && m.CPULoad1 >= 0 && m.MemoryAvailableBytes >= 0 && (m.WiFiSignalDBM == nil || (*m.WiFiSignalDBM >= -100 && *m.WiFiSignalDBM <= 0))
}

// Validate checks supported SLO names, safe identifiers, and non-negative aggregate values.
// Decision IDs are validated as fields rather than tags so repeated decisions do not create new series.
// Every current SLO is a magnitude, allowing zero but never negative baseline, optimized, or target values.
// This keeps product SLOs explicit rather than accepting arbitrary caller-defined metric names.
func (m ApplicationExperience) Validate() bool {
	return SafeTag(m.DeviceID) && SafeTag(m.Application) && validSLO(m.SLO) && SafeField(m.DecisionID) && m.BaselineValue >= 0 && m.OptimizedValue >= 0 && m.TargetValue >= 0
}

// Validate checks decision metadata and verifies that its candidate path comparison is unambiguous.
// It requires one through eight candidates and exactly one selected path in the submitted result.
// Reason, result, and rollback identifiers are constrained to safe metadata rather than free-form text.
// The method rejects invalid probe quality before the route explanation reaches the dashboard ledger.
func (d RouteDecision) Validate() bool {
	if !SafeField(d.DecisionID) || !SafeTag(d.DeviceID) || !SafeTag(d.Application) || !SafeTag(d.DestinationClass) || !SafeTag(d.PolicyID) || !SafeTag(d.Reason) || !SafeTag(d.Result) || !SafeTag(d.RollbackEvent) || len(d.CandidatePaths) == 0 || len(d.CandidatePaths) > 8 {
		return false
	}
	selected := 0
	for _, candidate := range d.CandidatePaths {
		if !candidate.Validate() {
			return false
		}
		if candidate.Selected {
			selected++
		}
	}
	return selected == 1
}

// Validate confirms one candidate path has safe identifiers and bounded reported quality values.
// It accepts latency, jitter, and loss from approved probes only when they are non-negative.
// Score follows the control-plane inclusive zero through one-hundred ranking convention.
// The containing RouteDecision enforces the cross-candidate rule that exactly one path is selected.
func (p CandidatePath) Validate() bool {
	return SafeTag(p.Name) && p.LatencyMS >= 0 && p.JitterMS >= 0 && p.LossPct >= 0 && p.LossPct <= 100 && p.Score >= 0 && p.Score <= 100
}

// SafeTag reports whether a value can safely become an InfluxDB tag identifier.
// It rejects line-protocol separators and record-breaking control characters before formatting.
// Callers use it for bounded dimensions such as application, path, and device category identifiers.
// It does not permit empty values because an empty tag creates ambiguous dashboard groupings.
func SafeTag(value string) bool {
	return value != "" && !strings.ContainsAny(value, ", =\n\r")
}

// SafeField reports whether a string can safely become a quoted InfluxDB field value.
// It rejects record-breaking control characters and bounds length to prevent oversized telemetry records.
// Quotes and backslashes remain allowed because the writer escapes them before line-protocol serialization.
// Decision IDs use fields rather than tags to avoid unbounded time-series cardinality.
func SafeField(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\n\r")
}

// validSLO limits application experience telemetry to the supported product objectives.
// The allowlist covers Zoom jitter and loss, Teams RTT, Git and GitHub transfer duration, and Slack reconnects.
// Stable names let Grafana panels and alert thresholds remain declarative and versionable.
// Adding an SLO requires an explicit source and dashboard change instead of free-form client input.
func validSLO(value string) bool {
	switch value {
	case "zoom_jitter_ms", "zoom_loss_pct", "teams_rtt_ms", "git_transfer_duration_ms", "github_transfer_duration_ms", "slack_reconnect_count":
		return true
	default:
		return false
	}
}
