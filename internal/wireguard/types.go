package wireguard

import "time"

type CreateRelayRequest struct {
	Name           string `json:"name"`
	PublicEndpoint string `json:"publicEndpoint"`
	ClientCIDR     string `json:"clientCidr"`
	ClientDNS      string `json:"clientDns"`
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
	RouteQuality     *RouteQuality `json:"routeQuality"`
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
	Revision      int64         `json:"revision"`
	InterfaceName string        `json:"interfaceName"`
	Peers         []DesiredPeer `json:"peers"`
}

type Heartbeat struct {
	ServerPublicKey string         `json:"serverPublicKey"`
	PublicEndpoint  string         `json:"publicEndpoint"`
	AppliedRevision int64          `json:"appliedRevision"`
	Peers           []PeerCounter  `json:"peers"`
	RoutingStatus   *RoutingStatus `json:"routingStatus"`
	RouteQuality    *RouteQuality  `json:"routeQuality"`
}

type RoutingStatus struct {
	Mode          string    `json:"mode"`
	RUPrefixCount int       `json:"ruPrefixCount"`
	UpdatedAt     time.Time `json:"updatedAt"`
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
