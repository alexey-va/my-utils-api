package wireguard

import "time"

type CreateRelayRequest struct {
	Name           string `json:"name"`
	PublicEndpoint string `json:"publicEndpoint"`
	ClientCIDR     string `json:"clientCidr"`
	ClientDNS      string `json:"clientDns"`
}

type UpdateExitPreferenceRequest struct {
	Preference string `json:"preference"`
}

type CreatePeerRequest struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

type UpdatePeerRequest struct {
	Name     *string `json:"name"`
	Category *string `json:"category"`
	Enabled  *bool   `json:"enabled"`
}

type PeerOrderItem struct {
	PeerID   string `json:"peerId"`
	Category string `json:"category"`
}

type UpdatePeerOrderRequest struct {
	Items []PeerOrderItem `json:"items"`
}

type CreatePeerCategoryRequest struct {
	Name string `json:"name"`
}

type UpdatePeerCategoryRequest struct {
	Name string `json:"name"`
}

type PeerCategoryOrderItem struct {
	CategoryID string `json:"categoryId"`
}

type UpdatePeerCategoryOrderRequest struct {
	Items []PeerCategoryOrderItem `json:"items"`
}

type RouteProbe struct {
	Target            string   `json:"target"`
	PacketLossPercent float64  `json:"packetLossPercent"`
	AverageRTTMs      *float64 `json:"averageRttMs"`
}

type RouteQuality struct {
	MeasuredAt time.Time  `json:"measuredAt"`
	Direct     RouteProbe `json:"direct"`
	Veesp      RouteProbe `json:"veesp"`
}

type ExitProbeHealth struct {
	ID                  string   `json:"id"`
	Interface           string   `json:"interface"`
	Healthy             bool     `json:"healthy"`
	Reason              *string  `json:"reason"`
	ExpectedEgressIP    string   `json:"expectedEgressIp"`
	ObservedEgressIP    *string  `json:"observedEgressIp"`
	HandshakeAtEpoch    int64    `json:"handshakeAtEpoch"`
	HandshakeAgeSeconds *int64   `json:"handshakeAgeSeconds"`
	LatencyMs           *float64 `json:"latencyMs"`
}

type ExitHealthCounter struct {
	Successes int `json:"successes"`
	Failures  int `json:"failures"`
}

type ExitHealth struct {
	SchemaVersion   int                          `json:"schemaVersion"`
	CheckedAt       time.Time                    `json:"checkedAt"`
	OverallStatus   string                       `json:"overallStatus"`
	ActiveExit      *string                      `json:"activeExit"`
	ActiveInterface *string                      `json:"activeInterface"`
	Changed         bool                         `json:"changed"`
	Counters        map[string]ExitHealthCounter `json:"counters"`
	Exits           map[string]ExitProbeHealth   `json:"exits"`
}

type Relay struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	PublicEndpoint   string        `json:"publicEndpoint"`
	ClientCIDR       string        `json:"clientCidr"`
	ClientDNS        string        `json:"clientDns"`
	InterfaceName    string        `json:"interfaceName"`
	ServerPublicKey  *string       `json:"serverPublicKey"`
	DesiredRevision  int64         `json:"desiredRevision"`
	AppliedRevision  *int64        `json:"appliedRevision"`
	Status           string        `json:"status"`
	LastSeenAt       *time.Time    `json:"lastSeenAt"`
	RoutingMode      string        `json:"routingMode"`
	RUPrefixCount    int           `json:"ruPrefixCount"`
	RoutingUpdatedAt *time.Time    `json:"routingUpdatedAt"`
	RoutingHealthy   *bool         `json:"routingHealthy"`
	RoutingCheckedAt *time.Time    `json:"routingCheckedAt"`
	RouteQuality     *RouteQuality `json:"routeQuality"`
	ExitHealth       *ExitHealth   `json:"exitHealth"`
	ExitPreference   string        `json:"exitPreference"`
	CreatedAt        time.Time     `json:"createdAt"`
	UpdatedAt        time.Time     `json:"updatedAt"`
}

type CreatedRelay struct {
	Relay
	AgentToken string `json:"agentToken"`
}

type AgentTokenResponse struct {
	AgentToken string `json:"agentToken"`
}

type Peer struct {
	ID                            string        `json:"id"`
	Name                          string        `json:"name"`
	Category                      string        `json:"category"`
	SortOrder                     int           `json:"sortOrder"`
	PublicKey                     string        `json:"publicKey"`
	AssignedIP                    string        `json:"assignedIp"`
	Enabled                       bool          `json:"enabled"`
	LatestHandshakeAt             *time.Time    `json:"latestHandshakeAt"`
	TotalReceiveBytes             int64         `json:"totalReceiveBytes"`
	TotalTransmitBytes            int64         `json:"totalTransmitBytes"`
	CurrentDownloadBytesPerSecond float64       `json:"currentDownloadBytesPerSecond"`
	CurrentUploadBytesPerSecond   float64       `json:"currentUploadBytesPerSecond"`
	MetricsUpdatedAt              *time.Time    `json:"metricsUpdatedAt"`
	Traffic                       PeriodTraffic `json:"traffic"`
	CreatedAt                     time.Time     `json:"createdAt"`
	UpdatedAt                     time.Time     `json:"updatedAt"`
}

type PeerCategory struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type PeerCredentials struct {
	Peer         Peer   `json:"peer"`
	ClientConfig string `json:"clientConfig"`
	FileName     string `json:"fileName"`
}

type DesiredPeer struct {
	PublicKey string `json:"publicKey"`
	AllowedIP string `json:"allowedIp"`
}

type DesiredState struct {
	Revision       int64         `json:"revision"`
	InterfaceName  string        `json:"interfaceName"`
	ExitPreference string        `json:"exitPreference"`
	Peers          []DesiredPeer `json:"peers"`
}

type Heartbeat struct {
	ServerPublicKey string         `json:"serverPublicKey"`
	PublicEndpoint  string         `json:"publicEndpoint"`
	AppliedRevision int64          `json:"appliedRevision"`
	Peers           []PeerCounter  `json:"peers"`
	RoutingStatus   *RoutingStatus `json:"routingStatus"`
	RouteQuality    *RouteQuality  `json:"routeQuality"`
	ExitHealth      *ExitHealth    `json:"exitHealth"`
}

type RoutingStatus struct {
	Mode          string    `json:"mode"`
	RUPrefixCount int       `json:"ruPrefixCount"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Healthy       bool      `json:"healthy"`
	CheckedAt     time.Time `json:"checkedAt"`
}

type PeerCounter struct {
	PublicKey                  string          `json:"publicKey"`
	LatestHandshakeEpochSecond int64           `json:"latestHandshakeEpochSeconds"`
	ReceiveBytes               int64           `json:"receiveBytes"`
	TransmitBytes              int64           `json:"transmitBytes"`
	RoutingTraffic             *RoutingTraffic `json:"routingTraffic"`
}

type RoutingTraffic struct {
	RUDownloadBytes    int64 `json:"ruDownloadBytes"`
	RUUploadBytes      int64 `json:"ruUploadBytes"`
	NonRUDownloadBytes int64 `json:"nonRuDownloadBytes"`
	NonRUUploadBytes   int64 `json:"nonRuUploadBytes"`
}

type MetricPoint struct {
	BucketStart        time.Time `json:"bucketStart"`
	DownloadBytes      int64     `json:"downloadBytes"`
	UploadBytes        int64     `json:"uploadBytes"`
	RUDownloadBytes    int64     `json:"ruDownloadBytes"`
	RUUploadBytes      int64     `json:"ruUploadBytes"`
	NonRUDownloadBytes int64     `json:"nonRuDownloadBytes"`
	NonRUUploadBytes   int64     `json:"nonRuUploadBytes"`
}

type TrafficTotals struct {
	DownloadBytes      int64 `json:"downloadBytes"`
	UploadBytes        int64 `json:"uploadBytes"`
	RUDownloadBytes    int64 `json:"ruDownloadBytes"`
	RUUploadBytes      int64 `json:"ruUploadBytes"`
	NonRUDownloadBytes int64 `json:"nonRuDownloadBytes"`
	NonRUUploadBytes   int64 `json:"nonRuUploadBytes"`
}

type PeriodTraffic struct {
	TrafficTotals
	Range string    `json:"range"`
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
}

type Metrics struct {
	PeerID  string        `json:"peerId"`
	Range   string        `json:"range"`
	From    time.Time     `json:"from"`
	To      time.Time     `json:"to"`
	Summary TrafficTotals `json:"summary"`
	Points  []MetricPoint `json:"points"`
}

type ExitHealthMetricPoint struct {
	BucketStart                  time.Time `json:"bucketStart"`
	PrimaryAvailabilityPercent   float64   `json:"primaryAvailabilityPercent"`
	SecondaryAvailabilityPercent float64   `json:"secondaryAvailabilityPercent"`
	PrimaryAverageLatencyMs      *float64  `json:"primaryAverageLatencyMs"`
	SecondaryAverageLatencyMs    *float64  `json:"secondaryAverageLatencyMs"`
	PrimaryFailureReason         *string   `json:"primaryFailureReason"`
	SecondaryFailureReason       *string   `json:"secondaryFailureReason"`
	ActiveExit                   *string   `json:"activeExit"`
	OverallStatus                string    `json:"overallStatus"`
	Samples                      int       `json:"samples"`
}

type ExitHealthHistory struct {
	Range  string                  `json:"range"`
	From   time.Time               `json:"from"`
	To     time.Time               `json:"to"`
	Points []ExitHealthMetricPoint `json:"points"`
}

type Snapshot struct {
	Relay             Relay              `json:"relay"`
	Categories        []PeerCategory     `json:"categories"`
	Peers             []Peer             `json:"peers"`
	PeerMetrics       map[string]Metrics `json:"peerMetrics"`
	ExitHealthHistory ExitHealthHistory  `json:"exitHealthHistory"`
}
