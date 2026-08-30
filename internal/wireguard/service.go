package wireguard

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const relayColumns = `id::text,name,public_endpoint,client_cidr,client_dns,interface_name,agent_token_hash,server_public_key,desired_revision,applied_revision,last_seen_at,routing_mode,ru_prefix_count,routing_updated_at,routing_healthy,routing_checked_at,exit_health,exit_preference,direct_probe_target,direct_packet_loss_percent,direct_average_rtt_ms,veesp_probe_target,veesp_packet_loss_percent,veesp_average_rtt_ms,route_quality_updated_at,created_at,updated_at`
const defaultPeerCategory = "Пользовательские"

var defaultPeerCategories = []string{defaultPeerCategory, "Служебные"}

type Service struct {
	pool                 *pgxpool.Pool
	cipher               *CredentialsCipher
	clock                func() time.Time
	retentionMu          sync.Mutex
	lastRetentionCleanup map[string]time.Time
}

func NewService(pool *pgxpool.Pool, cipher *CredentialsCipher) *Service {
	return &Service{
		pool:                 pool,
		cipher:               cipher,
		clock:                time.Now,
		lastRetentionCleanup: make(map[string]time.Time),
	}
}

type relayRecord struct {
	Relay
	TokenHash                                  string
	DirectTarget, VeespTarget                  *string
	DirectLoss, DirectRTT, VeespLoss, VeespRTT *float64
	QualityUpdated                             *time.Time
	ExitHealthJSON                             []byte
}

type row interface{ Scan(...any) error }

func scanRelay(source row, now time.Time) (relayRecord, error) {
	var value relayRecord
	err := source.Scan(
		&value.ID, &value.Name, &value.PublicEndpoint, &value.ClientCIDR, &value.ClientDNS,
		&value.InterfaceName, &value.TokenHash, &value.ServerPublicKey, &value.DesiredRevision,
		&value.AppliedRevision, &value.LastSeenAt, &value.RoutingMode, &value.RUPrefixCount,
		&value.RoutingUpdatedAt, &value.RoutingHealthy, &value.RoutingCheckedAt, &value.ExitHealthJSON, &value.ExitPreference,
		&value.DirectTarget, &value.DirectLoss, &value.DirectRTT,
		&value.VeespTarget, &value.VeespLoss, &value.VeespRTT, &value.QualityUpdated,
		&value.CreatedAt, &value.UpdatedAt,
	)
	if err != nil {
		return relayRecord{}, err
	}
	if len(value.ExitHealthJSON) > 0 {
		var exitHealth ExitHealth
		if err := json.Unmarshal(value.ExitHealthJSON, &exitHealth); err != nil {
			return relayRecord{}, fmt.Errorf("decode relay exit health: %w", err)
		}
		value.ExitHealth = &exitHealth
	}
	value.Status = relayStatus(value.Relay, now)
	if value.QualityUpdated != nil && value.DirectTarget != nil && value.DirectLoss != nil && value.VeespTarget != nil && value.VeespLoss != nil {
		value.RouteQuality = &RouteQuality{
			MeasuredAt: *value.QualityUpdated,
			Direct:     RouteProbe{Target: *value.DirectTarget, PacketLossPercent: *value.DirectLoss, AverageRTTMs: value.DirectRTT},
			Veesp:      RouteProbe{Target: *value.VeespTarget, PacketLossPercent: *value.VeespLoss, AverageRTTMs: value.VeespRTT},
		}
	}
	return value, nil
}

func (s *Service) ListRelays(ctx context.Context) ([]Relay, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+relayColumns+` FROM wireguard_relays ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Relay, 0)
	for rows.Next() {
		record, err := scanRelay(rows, s.now())
		if err != nil {
			return nil, err
		}
		result = append(result, record.Relay)
	}
	return result, rows.Err()
}

func (s *Service) CreateRelay(ctx context.Context, body CreateRelayRequest) (CreatedRelay, error) {
	name, err := requiredText(body.Name, "Relay name", 80)
	if err != nil {
		return CreatedRelay{}, err
	}
	endpoint, err := validateEndpoint(body.PublicEndpoint)
	if err != nil {
		return CreatedRelay{}, err
	}
	cidr, err := ParseCIDR(body.ClientCIDR)
	if err != nil {
		return CreatedRelay{}, badRequest(err.Error())
	}
	dns, err := validateIPv4(body.ClientDNS, "Client DNS")
	if err != nil {
		return CreatedRelay{}, err
	}
	token, err := generateToken()
	if err != nil {
		return CreatedRelay{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CreatedRelay{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var duplicate bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wireguard_relays WHERE lower(name)=lower($1))`, name).Scan(&duplicate); err != nil {
		return CreatedRelay{}, err
	}
	if duplicate {
		return CreatedRelay{}, conflict("Relay name already exists")
	}
	now := s.now()
	record, err := scanRelay(tx.QueryRow(ctx, `INSERT INTO wireguard_relays(id,name,public_endpoint,client_cidr,client_dns,interface_name,agent_token_hash,created_at,updated_at) VALUES(gen_random_uuid(),$1,$2,$3,$4,'wg-users',$5,$6,$6) RETURNING `+relayColumns, name, endpoint, cidr.String(), dns, tokenHash(token), now), now)
	if err != nil {
		return CreatedRelay{}, err
	}
	for sortOrder, category := range defaultPeerCategories {
		if _, err := tx.Exec(ctx, `INSERT INTO wireguard_peer_categories(relay_id,name,sort_order,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,$4)`, record.ID, category, sortOrder, now); err != nil {
			return CreatedRelay{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedRelay{}, err
	}
	return CreatedRelay{Relay: record.Relay, AgentToken: token}, nil
}

func (s *Service) RotateToken(ctx context.Context, relayID string) (AgentTokenResponse, error) {
	if _, err := s.relay(ctx, relayID); err != nil {
		return AgentTokenResponse{}, err
	}
	token, err := generateToken()
	if err != nil {
		return AgentTokenResponse{}, err
	}
	_, err = s.pool.Exec(ctx, `UPDATE wireguard_relays SET agent_token_hash=$2,updated_at=$3 WHERE id=$1::uuid`, relayID, tokenHash(token), s.now())
	return AgentTokenResponse{AgentToken: token}, err
}

func (s *Service) DeleteRelay(ctx context.Context, relayID string) error {
	if _, err := s.relay(ctx, relayID); err != nil {
		return err
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM wireguard_peers WHERE relay_id=$1::uuid`, relayID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return conflict("Delete relay peers first")
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM wireguard_relays WHERE id=$1::uuid`, relayID)
	return err
}

type peerRecord struct {
	Peer
	Ciphertext, Nonce                                            string
	RawReceive, RawTransmit                                      int64
	RawRUDownload, RawRUUpload, RawNonRUDownload, RawNonRUUpload int64
}

const peerCategoryColumns = `id::text,name,sort_order,created_at,updated_at`

func scanPeerCategory(source row) (PeerCategory, error) {
	var value PeerCategory
	err := source.Scan(&value.ID, &value.Name, &value.SortOrder, &value.CreatedAt, &value.UpdatedAt)
	return value, err
}

type rowsQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func listPeerCategories(ctx context.Context, source rowsQuerier, relayID string) ([]PeerCategory, error) {
	rows, err := source.Query(ctx, `SELECT `+peerCategoryColumns+` FROM wireguard_peer_categories WHERE relay_id=$1::uuid ORDER BY sort_order ASC,created_at ASC`, relayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PeerCategory, 0)
	for rows.Next() {
		category, err := scanPeerCategory(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, category)
	}
	return result, rows.Err()
}

func (s *Service) ListPeerCategories(ctx context.Context, relayID string) ([]PeerCategory, error) {
	if _, err := s.relay(ctx, relayID); err != nil {
		return nil, err
	}
	return listPeerCategories(ctx, s.pool, relayID)
}

func lockRelayTx(ctx context.Context, tx pgx.Tx, relayID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM wireguard_relays WHERE id=$1::uuid FOR UPDATE`, relayID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound("WireGuard relay not found")
	}
	return err
}

func ensurePeerCategoryTx(ctx context.Context, tx pgx.Tx, relayID, requestedName string, now time.Time) (string, error) {
	name, err := requiredText(requestedName, "Peer category", 80)
	if err != nil {
		return "", err
	}
	category, err := scanPeerCategory(tx.QueryRow(ctx, `SELECT `+peerCategoryColumns+` FROM wireguard_peer_categories WHERE relay_id=$1::uuid AND lower(name)=lower($2)`, relayID, name))
	if err == nil {
		return category.Name, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	var sortOrder int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sort_order),-1)+1 FROM wireguard_peer_categories WHERE relay_id=$1::uuid`, relayID).Scan(&sortOrder); err != nil {
		return "", err
	}
	category, err = scanPeerCategory(tx.QueryRow(ctx, `INSERT INTO wireguard_peer_categories(relay_id,name,sort_order,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,$4) RETURNING `+peerCategoryColumns, relayID, name, sortOrder, now))
	if err != nil {
		return "", err
	}
	return category.Name, nil
}

func (s *Service) CreatePeerCategory(ctx context.Context, relayID string, body CreatePeerCategoryRequest) (PeerCategory, error) {
	name, err := requiredText(body.Name, "Peer category", 80)
	if err != nil {
		return PeerCategory{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PeerCategory{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := lockRelayTx(ctx, tx, relayID); err != nil {
		return PeerCategory{}, err
	}
	var duplicate bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wireguard_peer_categories WHERE relay_id=$1::uuid AND lower(name)=lower($2))`, relayID, name).Scan(&duplicate); err != nil {
		return PeerCategory{}, err
	}
	if duplicate {
		return PeerCategory{}, conflict("Peer category already exists")
	}
	var sortOrder int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sort_order),-1)+1 FROM wireguard_peer_categories WHERE relay_id=$1::uuid`, relayID).Scan(&sortOrder); err != nil {
		return PeerCategory{}, err
	}
	now := s.now()
	category, err := scanPeerCategory(tx.QueryRow(ctx, `INSERT INTO wireguard_peer_categories(relay_id,name,sort_order,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,$4) RETURNING `+peerCategoryColumns, relayID, name, sortOrder, now))
	if err != nil {
		return PeerCategory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PeerCategory{}, err
	}
	return category, nil
}

func (s *Service) UpdatePeerCategory(ctx context.Context, relayID, categoryID string, body UpdatePeerCategoryRequest) (PeerCategory, error) {
	name, err := requiredText(body.Name, "Peer category", 80)
	if err != nil {
		return PeerCategory{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PeerCategory{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := lockRelayTx(ctx, tx, relayID); err != nil {
		return PeerCategory{}, err
	}
	category, err := scanPeerCategory(tx.QueryRow(ctx, `SELECT `+peerCategoryColumns+` FROM wireguard_peer_categories WHERE id=$1::uuid AND relay_id=$2::uuid FOR UPDATE`, categoryID, relayID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PeerCategory{}, notFound("WireGuard peer category not found")
	}
	if err != nil {
		return PeerCategory{}, err
	}
	var duplicate bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wireguard_peer_categories WHERE relay_id=$1::uuid AND id<>$2::uuid AND lower(name)=lower($3))`, relayID, categoryID, name).Scan(&duplicate); err != nil {
		return PeerCategory{}, err
	}
	if duplicate {
		return PeerCategory{}, conflict("Peer category already exists")
	}
	if category.Name != name {
		now := s.now()
		previousName := category.Name
		category, err = scanPeerCategory(tx.QueryRow(ctx, `UPDATE wireguard_peer_categories SET name=$3,updated_at=$4 WHERE id=$1::uuid AND relay_id=$2::uuid RETURNING `+peerCategoryColumns, categoryID, relayID, name, now))
		if err != nil {
			return PeerCategory{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE wireguard_peers SET category=$3,updated_at=$4 WHERE relay_id=$1::uuid AND lower(category)=lower($2)`, relayID, previousName, name, now); err != nil {
			return PeerCategory{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PeerCategory{}, err
	}
	return category, nil
}

func (s *Service) ReorderPeerCategories(ctx context.Context, relayID string, body UpdatePeerCategoryOrderRequest) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := lockRelayTx(ctx, tx, relayID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id::text FROM wireguard_peer_categories WHERE relay_id=$1::uuid FOR UPDATE`, relayID)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existing[id] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(body.Items) != len(existing) {
		return conflict("Peer category list changed; refresh and try again")
	}
	seen := make(map[string]bool, len(body.Items))
	now := s.now()
	for sortOrder, item := range body.Items {
		if !existing[item.CategoryID] || seen[item.CategoryID] {
			return conflict("Peer category list changed; refresh and try again")
		}
		seen[item.CategoryID] = true
		if _, err := tx.Exec(ctx, `UPDATE wireguard_peer_categories SET sort_order=$3,updated_at=$4 WHERE id=$1::uuid AND relay_id=$2::uuid`, item.CategoryID, relayID, sortOrder, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) DeletePeerCategory(ctx context.Context, relayID, categoryID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := lockRelayTx(ctx, tx, relayID); err != nil {
		return err
	}
	category, err := scanPeerCategory(tx.QueryRow(ctx, `SELECT `+peerCategoryColumns+` FROM wireguard_peer_categories WHERE id=$1::uuid AND relay_id=$2::uuid FOR UPDATE`, categoryID, relayID))
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound("WireGuard peer category not found")
	}
	if err != nil {
		return err
	}
	var peerCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM wireguard_peers WHERE relay_id=$1::uuid AND lower(category)=lower($2)`, relayID, category.Name).Scan(&peerCount); err != nil {
		return err
	}
	if peerCount > 0 {
		return conflict("Move peer devices out of the category first")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM wireguard_peer_categories WHERE id=$1::uuid AND relay_id=$2::uuid`, categoryID, relayID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `WITH ordered AS (
		SELECT id,row_number() OVER (ORDER BY sort_order,created_at,id)-1 AS position
		FROM wireguard_peer_categories
		WHERE relay_id=$1::uuid
	) UPDATE wireguard_peer_categories AS category
	SET sort_order=ordered.position,updated_at=$2
	FROM ordered
	WHERE category.id=ordered.id`, relayID, s.now()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const peerColumns = `id::text,name,category,sort_order,public_key,assigned_ip,enabled,latest_handshake_at,total_receive_bytes,total_transmit_bytes,current_download_bytes_per_second,current_upload_bytes_per_second,metrics_updated_at,created_at,updated_at,private_key_ciphertext,private_key_nonce,raw_receive_bytes,raw_transmit_bytes,raw_ru_download_bytes,raw_ru_upload_bytes,raw_non_ru_download_bytes,raw_non_ru_upload_bytes`

func scanPeer(source row) (peerRecord, error) {
	var value peerRecord
	err := source.Scan(&value.ID, &value.Name, &value.Category, &value.SortOrder, &value.PublicKey, &value.AssignedIP, &value.Enabled, &value.LatestHandshakeAt, &value.TotalReceiveBytes, &value.TotalTransmitBytes, &value.CurrentDownloadBytesPerSecond, &value.CurrentUploadBytesPerSecond, &value.MetricsUpdatedAt, &value.CreatedAt, &value.UpdatedAt, &value.Ciphertext, &value.Nonce, &value.RawReceive, &value.RawTransmit, &value.RawRUDownload, &value.RawRUUpload, &value.RawNonRUDownload, &value.RawNonRUUpload)
	return value, err
}

func (s *Service) ListPeers(ctx context.Context, relayID, rangeName string) ([]Peer, error) {
	if _, err := s.relay(ctx, relayID); err != nil {
		return nil, err
	}
	rangeName, from, to, _, err := metricRange(rangeName, s.now())
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+peerColumns+` FROM wireguard_peers WHERE relay_id=$1::uuid ORDER BY sort_order ASC,created_at ASC`, relayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Peer, 0)
	for rows.Next() {
		value, err := scanPeer(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value.Peer)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	totals, err := s.peerTrafficTotals(ctx, relayID, from, to)
	if err != nil {
		return nil, err
	}
	for index := range result {
		result[index].Traffic = PeriodTraffic{TrafficTotals: totals[result[index].ID], Range: rangeName, From: from, To: to}
	}
	return result, nil
}

func (s *Service) CreatePeer(ctx context.Context, relayID string, body CreatePeerRequest) (PeerCredentials, error) {
	if s.cipher == nil || !s.cipher.Configured() {
		return PeerCredentials{}, unavailable("WireGuard credential encryption is not configured")
	}
	name, err := requiredText(body.Name, "Peer name", 120)
	if err != nil {
		return PeerCredentials{}, err
	}
	category := strings.TrimSpace(body.Category)
	if category == "" {
		category = defaultPeerCategory
	}
	category, err = requiredText(category, "Peer category", 80)
	if err != nil {
		return PeerCredentials{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PeerCredentials{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	relay, err := scanRelay(tx.QueryRow(ctx, `SELECT `+relayColumns+` FROM wireguard_relays WHERE id=$1::uuid FOR UPDATE`, relayID), s.now())
	if errors.Is(err, pgx.ErrNoRows) {
		return PeerCredentials{}, notFound("WireGuard relay not found")
	}
	if err != nil {
		return PeerCredentials{}, err
	}
	if relay.ServerPublicKey == nil {
		return PeerCredentials{}, conflict("Relay has not reported its server public key")
	}
	var duplicate bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wireguard_peers WHERE relay_id=$1::uuid AND lower(name)=lower($2))`, relayID, name).Scan(&duplicate); err != nil {
		return PeerCredentials{}, err
	}
	if duplicate {
		return PeerCredentials{}, conflict("Peer name already exists")
	}
	cidr, err := ParseCIDR(relay.ClientCIDR)
	if err != nil {
		return PeerCredentials{}, err
	}
	rows, err := tx.Query(ctx, `SELECT assigned_ip FROM wireguard_peers WHERE relay_id=$1::uuid`, relayID)
	if err != nil {
		return PeerCredentials{}, err
	}
	used := map[string]bool{}
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			rows.Close()
			return PeerCredentials{}, err
		}
		used[ip] = true
	}
	rows.Close()
	assigned := ""
	for offset := 2; offset <= cidr.LastUsableHostOffset(); offset++ {
		ip, _ := cidr.HostAddress(offset)
		if !used[ip] {
			assigned = ip
			break
		}
	}
	if assigned == "" {
		return PeerCredentials{}, conflict("Relay client CIDR is exhausted")
	}
	pair, err := GenerateKeyPair()
	if err != nil {
		return PeerCredentials{}, err
	}
	encrypted, err := s.cipher.Encrypt(pair.PrivateKey)
	if err != nil {
		return PeerCredentials{}, err
	}
	now := s.now()
	category, err = ensurePeerCategoryTx(ctx, tx, relayID, category, now)
	if err != nil {
		return PeerCredentials{}, err
	}
	var sortOrder int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sort_order),-1)+1 FROM wireguard_peers WHERE relay_id=$1::uuid`, relayID).Scan(&sortOrder); err != nil {
		return PeerCredentials{}, err
	}
	peer, err := scanPeer(tx.QueryRow(ctx, `INSERT INTO wireguard_peers(id,relay_id,name,category,sort_order,public_key,private_key_ciphertext,private_key_nonce,assigned_ip,created_at,updated_at) VALUES(gen_random_uuid(),$1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$9) RETURNING `+peerColumns, relayID, name, category, sortOrder, pair.PublicKey, encrypted.Ciphertext, encrypted.Nonce, assigned, now))
	if err != nil {
		return PeerCredentials{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE wireguard_relays SET desired_revision=desired_revision+1,updated_at=$2 WHERE id=$1::uuid`, relayID, now); err != nil {
		return PeerCredentials{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PeerCredentials{}, err
	}
	return credentials(peer, relay.Relay, pair.PrivateKey, *relay.ServerPublicKey)
}

func (s *Service) Credentials(ctx context.Context, relayID, peerID string) (PeerCredentials, error) {
	relay, err := s.relay(ctx, relayID)
	if err != nil {
		return PeerCredentials{}, err
	}
	if relay.ServerPublicKey == nil {
		return PeerCredentials{}, conflict("Relay has not reported its server public key")
	}
	if s.cipher == nil || !s.cipher.Configured() {
		return PeerCredentials{}, unavailable("WireGuard credential encryption is not configured")
	}
	peer, err := s.peer(ctx, relayID, peerID)
	if err != nil {
		return PeerCredentials{}, err
	}
	private, err := s.cipher.Decrypt(EncryptedSecret{Ciphertext: peer.Ciphertext, Nonce: peer.Nonce})
	if err != nil {
		return PeerCredentials{}, err
	}
	return credentials(peer, relay.Relay, private, *relay.ServerPublicKey)
}

func credentials(peer peerRecord, relay Relay, private, serverPublic string) (PeerCredentials, error) {
	config, err := RenderClientConfig(private, peer.AssignedIP, relay.ClientDNS, serverPublic, relay.PublicEndpoint)
	if err != nil {
		return PeerCredentials{}, err
	}
	return PeerCredentials{Peer: peer.Peer, ClientConfig: config, FileName: fileSlug(peer.Name, peer.ID) + ".conf"}, nil
}

func (s *Service) UpdatePeer(ctx context.Context, relayID, peerID string, body UpdatePeerRequest) (Peer, error) {
	if body.Enabled == nil && body.Name == nil && body.Category == nil {
		return Peer{}, badRequest("Peer update is empty")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Peer{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := lockRelayTx(ctx, tx, relayID); err != nil {
		return Peer{}, err
	}
	peer, err := scanPeer(tx.QueryRow(ctx, `SELECT `+peerColumns+` FROM wireguard_peers WHERE id=$1::uuid AND relay_id=$2::uuid FOR UPDATE`, peerID, relayID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Peer{}, notFound("WireGuard peer not found")
	}
	if err != nil {
		return Peer{}, err
	}
	name := peer.Name
	if body.Name != nil {
		name, err = requiredText(*body.Name, "Peer name", 120)
		if err != nil {
			return Peer{}, err
		}
		var duplicate bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wireguard_peers WHERE relay_id=$1::uuid AND id<>$2::uuid AND lower(name)=lower($3))`, relayID, peerID, name).Scan(&duplicate); err != nil {
			return Peer{}, err
		}
		if duplicate {
			return Peer{}, conflict("Peer name already exists")
		}
	}
	category := peer.Category
	if body.Category != nil {
		category, err = ensurePeerCategoryTx(ctx, tx, relayID, *body.Category, s.now())
		if err != nil {
			return Peer{}, err
		}
	}
	enabled := peer.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	metadataChanged := peer.Name != name || peer.Category != category
	enabledChanged := peer.Enabled != enabled
	if metadataChanged || enabledChanged {
		now := s.now()
		peer.Name = name
		peer.Category = category
		peer.Enabled = enabled
		peer.UpdatedAt = now
		if _, err := tx.Exec(ctx, `UPDATE wireguard_peers SET name=$2,category=$3,enabled=$4,updated_at=$5 WHERE id=$1::uuid`, peerID, name, category, enabled, now); err != nil {
			return Peer{}, err
		}
		if enabledChanged {
			if _, err := tx.Exec(ctx, `UPDATE wireguard_relays SET desired_revision=desired_revision+1,updated_at=$2 WHERE id=$1::uuid`, relayID, now); err != nil {
				return Peer{}, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Peer{}, err
	}
	return peer.Peer, nil
}

func (s *Service) ReorderPeers(ctx context.Context, relayID string, body UpdatePeerOrderRequest) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := lockRelayTx(ctx, tx, relayID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id::text FROM wireguard_peers WHERE relay_id=$1::uuid FOR UPDATE`, relayID)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existing[id] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(body.Items) != len(existing) {
		return conflict("Peer list changed; refresh and try again")
	}
	seen := make(map[string]bool, len(body.Items))
	for index, item := range body.Items {
		if !existing[item.PeerID] || seen[item.PeerID] {
			return conflict("Peer list changed; refresh and try again")
		}
		category, err := ensurePeerCategoryTx(ctx, tx, relayID, item.Category, s.now())
		if err != nil {
			return err
		}
		seen[item.PeerID] = true
		if _, err := tx.Exec(ctx, `UPDATE wireguard_peers SET category=$3,sort_order=$4,updated_at=$5 WHERE id=$1::uuid AND relay_id=$2::uuid`, item.PeerID, relayID, category, index, s.now()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) DeletePeer(ctx context.Context, relayID, peerID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
		return err
	}
	// Lock the relay first, matching Heartbeat's lock order. This avoids a
	// relay -> peer / peer -> relay deadlock when the agent reports counters
	// while an administrator removes a peer.
	if _, err := tx.Exec(ctx, `UPDATE wireguard_relays SET desired_revision=desired_revision+1,updated_at=$2 WHERE id=$1::uuid`, relayID, s.now()); err != nil {
		return peerMutationError(err)
	}
	result, err := tx.Exec(ctx, `DELETE FROM wireguard_peers WHERE id=$1::uuid AND relay_id=$2::uuid`, peerID, relayID)
	if err != nil {
		return peerMutationError(err)
	}
	if result.RowsAffected() == 0 {
		return notFound("WireGuard peer not found")
	}
	return tx.Commit(ctx)
}

func (s *Service) UpdateExitPreference(ctx context.Context, relayID string, body UpdateExitPreferenceRequest) (Relay, error) {
	preference := strings.ToUpper(strings.TrimSpace(body.Preference))
	if preference != "AUTO" && preference != "PRIMARY" && preference != "SECONDARY" {
		return Relay{}, badRequest("Exit preference is invalid")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Relay{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	relay, err := scanRelay(tx.QueryRow(ctx, `SELECT `+relayColumns+` FROM wireguard_relays WHERE id=$1::uuid FOR UPDATE`, relayID), s.now())
	if errors.Is(err, pgx.ErrNoRows) {
		return Relay{}, notFound("WireGuard relay not found")
	}
	if err != nil {
		return Relay{}, err
	}
	if preference != "AUTO" {
		if relay.ExitHealth == nil || relay.ExitHealth.CheckedAt.After(s.now().Add(5*time.Second)) || s.now().Sub(relay.ExitHealth.CheckedAt) > 45*time.Second {
			return Relay{}, conflict("Selected exit is not healthy")
		}
		exitID := strings.ToLower(preference)
		probe, ok := relay.ExitHealth.Exits[exitID]
		if !ok || !probe.Healthy {
			return Relay{}, conflict("Selected exit is not healthy")
		}
	}
	if relay.ExitPreference == preference {
		return relay.Relay, tx.Commit(ctx)
	}
	updated, err := scanRelay(tx.QueryRow(ctx, `UPDATE wireguard_relays SET exit_preference=$2,desired_revision=desired_revision+1,updated_at=$3 WHERE id=$1::uuid RETURNING `+relayColumns, relayID, preference, s.now()), s.now())
	if err != nil {
		return Relay{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Relay{}, err
	}
	return updated.Relay, nil
}

func (s *Service) Desired(ctx context.Context, relayID string) (DesiredState, error) {
	relay, err := s.relay(ctx, relayID)
	if err != nil {
		return DesiredState{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT public_key,assigned_ip FROM wireguard_peers WHERE relay_id=$1::uuid AND enabled ORDER BY assigned_ip ASC`, relayID)
	if err != nil {
		return DesiredState{}, err
	}
	defer rows.Close()
	result := DesiredState{Revision: relay.DesiredRevision, InterfaceName: relay.InterfaceName, ExitPreference: relay.ExitPreference, Peers: []DesiredPeer{}}
	for rows.Next() {
		var peer DesiredPeer
		if err := rows.Scan(&peer.PublicKey, &peer.AllowedIP); err != nil {
			return DesiredState{}, err
		}
		peer.AllowedIP += "/32"
		result.Peers = append(result.Peers, peer)
	}
	return result, rows.Err()
}

func (s *Service) Heartbeat(ctx context.Context, relayID string, body Heartbeat) error {
	serverKey, err := validatePublicKey(body.ServerPublicKey)
	if err != nil {
		return err
	}
	endpoint, err := validateEndpoint(body.PublicEndpoint)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	relay, err := scanRelay(tx.QueryRow(ctx, `SELECT `+relayColumns+` FROM wireguard_relays WHERE id=$1::uuid FOR UPDATE`, relayID), s.now())
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound("WireGuard relay not found")
	}
	if err != nil {
		return err
	}
	if body.AppliedRevision < 0 || body.AppliedRevision > relay.DesiredRevision {
		return badRequest("Applied revision is invalid")
	}
	now := s.now()
	if body.RoutingStatus != nil {
		r := body.RoutingStatus
		if (r.Mode != "AWG_ONLY" && r.Mode != "RU_DIRECT_AWG_DEFAULT") || r.RUPrefixCount < 0 || r.RUPrefixCount > 100000 || (r.Mode == "AWG_ONLY" && r.RUPrefixCount != 0) || (r.Mode == "RU_DIRECT_AWG_DEFAULT" && r.RUPrefixCount == 0) || r.UpdatedAt.After(now.Add(5*time.Minute)) || r.CheckedAt.IsZero() || r.CheckedAt.After(now.Add(5*time.Minute)) {
			return badRequest("Routing status is invalid")
		}
		relay.RoutingMode = r.Mode
		relay.RUPrefixCount = r.RUPrefixCount
		relay.RoutingUpdatedAt = &r.UpdatedAt
		relay.RoutingHealthy = &r.Healthy
		relay.RoutingCheckedAt = &r.CheckedAt
	}
	if body.RouteQuality != nil {
		q := body.RouteQuality
		if q.MeasuredAt.After(now.Add(5 * time.Minute)) {
			return badRequest("Route quality timestamp is invalid")
		}
		if err := validateProbe(q.Direct); err != nil {
			return err
		}
		if err := validateProbe(q.Veesp); err != nil {
			return err
		}
		relay.DirectTarget = &q.Direct.Target
		relay.DirectLoss = &q.Direct.PacketLossPercent
		relay.DirectRTT = q.Direct.AverageRTTMs
		relay.VeespTarget = &q.Veesp.Target
		relay.VeespLoss = &q.Veesp.PacketLossPercent
		relay.VeespRTT = q.Veesp.AverageRTTMs
		relay.QualityUpdated = &q.MeasuredAt
	}
	if err := validateExitHealth(body.ExitHealth, now); err != nil {
		return err
	}
	if body.ExitHealth != nil {
		relay.ExitHealth = body.ExitHealth
		primary := body.ExitHealth.Exits["primary"]
		secondary := body.ExitHealth.Exits["secondary"]
		if _, err := tx.Exec(ctx, `INSERT INTO wireguard_exit_health_samples(id,relay_id,recorded_at,overall_status,active_exit,primary_healthy,primary_latency_ms,primary_failure_reason,secondary_healthy,secondary_latency_ms,secondary_failure_reason) VALUES(gen_random_uuid(),$1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, relayID, now, body.ExitHealth.OverallStatus, body.ExitHealth.ActiveExit, primary.Healthy, primary.LatencyMs, primary.Reason, secondary.Healthy, secondary.LatencyMs, secondary.Reason); err != nil {
			return err
		}
	}
	var exitHealthJSON any
	if relay.ExitHealth != nil {
		encoded, err := json.Marshal(relay.ExitHealth)
		if err != nil {
			return fmt.Errorf("encode relay exit health: %w", err)
		}
		exitHealthJSON = string(encoded)
	}
	_, err = tx.Exec(ctx, `UPDATE wireguard_relays SET server_public_key=$2,public_endpoint=$3,applied_revision=$4,last_seen_at=$5,updated_at=$5,routing_mode=$6,ru_prefix_count=$7,routing_updated_at=$8,routing_healthy=$9,routing_checked_at=$10,exit_health=$11::jsonb,direct_probe_target=$12,direct_packet_loss_percent=$13,direct_average_rtt_ms=$14,veesp_probe_target=$15,veesp_packet_loss_percent=$16,veesp_average_rtt_ms=$17,route_quality_updated_at=$18 WHERE id=$1::uuid`, relayID, serverKey, endpoint, body.AppliedRevision, now, relay.RoutingMode, relay.RUPrefixCount, relay.RoutingUpdatedAt, relay.RoutingHealthy, relay.RoutingCheckedAt, exitHealthJSON, relay.DirectTarget, relay.DirectLoss, relay.DirectRTT, relay.VeespTarget, relay.VeespLoss, relay.VeespRTT, relay.QualityUpdated)
	if err != nil {
		return err
	}
	for _, counter := range body.Peers {
		if _, err := validatePublicKey(counter.PublicKey); err != nil {
			return err
		}
		if counter.LatestHandshakeEpochSecond < 0 || counter.ReceiveBytes < 0 || counter.TransmitBytes < 0 {
			return badRequest("Peer counters must not be negative")
		}
		if counter.RoutingTraffic != nil {
			t := counter.RoutingTraffic
			if t.RUDownloadBytes < 0 || t.RUUploadBytes < 0 || t.NonRUDownloadBytes < 0 || t.NonRUUploadBytes < 0 {
				return badRequest("Peer routing counters must not be negative")
			}
		}
		peer, err := scanPeer(tx.QueryRow(ctx, `SELECT `+peerColumns+` FROM wireguard_peers WHERE relay_id=$1::uuid AND public_key=$2 FOR UPDATE`, relayID, counter.PublicKey))
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		receiveDelta := counterDelta(peer.RawReceive, counter.ReceiveBytes)
		transmitDelta := counterDelta(peer.RawTransmit, counter.TransmitBytes)
		peer.TotalReceiveBytes += receiveDelta
		peer.TotalTransmitBytes += transmitDelta
		downloadRate, uploadRate := 0.0, 0.0
		if peer.MetricsUpdatedAt != nil {
			elapsedSeconds := now.Sub(*peer.MetricsUpdatedAt).Seconds()
			if elapsedSeconds > 0 {
				downloadRate = float64(transmitDelta) / elapsedSeconds
				uploadRate = float64(receiveDelta) / elapsedSeconds
			}
		}
		rawTraffic := RoutingTraffic{
			RUDownloadBytes: peer.RawRUDownload, RUUploadBytes: peer.RawRUUpload,
			NonRUDownloadBytes: peer.RawNonRUDownload, NonRUUploadBytes: peer.RawNonRUUpload,
		}
		trafficDelta := RoutingTraffic{}
		if counter.RoutingTraffic != nil {
			trafficDelta = RoutingTraffic{
				RUDownloadBytes:    counterDelta(peer.RawRUDownload, counter.RoutingTraffic.RUDownloadBytes),
				RUUploadBytes:      counterDelta(peer.RawRUUpload, counter.RoutingTraffic.RUUploadBytes),
				NonRUDownloadBytes: counterDelta(peer.RawNonRUDownload, counter.RoutingTraffic.NonRUDownloadBytes),
				NonRUUploadBytes:   counterDelta(peer.RawNonRUUpload, counter.RoutingTraffic.NonRUUploadBytes),
			}
			rawTraffic = *counter.RoutingTraffic
		}
		var handshake *time.Time
		if counter.LatestHandshakeEpochSecond > 0 {
			v := time.Unix(counter.LatestHandshakeEpochSecond, 0).UTC()
			handshake = &v
		} else {
			handshake = peer.LatestHandshakeAt
		}
		if _, err := tx.Exec(ctx, `UPDATE wireguard_peers SET raw_receive_bytes=$2,raw_transmit_bytes=$3,total_receive_bytes=$4,total_transmit_bytes=$5,current_download_bytes_per_second=$6,current_upload_bytes_per_second=$7,raw_ru_download_bytes=$8,raw_ru_upload_bytes=$9,raw_non_ru_download_bytes=$10,raw_non_ru_upload_bytes=$11,latest_handshake_at=$12,metrics_updated_at=$13,updated_at=$13 WHERE id=$1::uuid`, peer.ID, counter.ReceiveBytes, counter.TransmitBytes, peer.TotalReceiveBytes, peer.TotalTransmitBytes, downloadRate, uploadRate, rawTraffic.RUDownloadBytes, rawTraffic.RUUploadBytes, rawTraffic.NonRUDownloadBytes, rawTraffic.NonRUUploadBytes, handshake, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO wireguard_peer_metric_samples(id,peer_id,recorded_at,download_bytes,upload_bytes,ru_download_bytes,ru_upload_bytes,non_ru_download_bytes,non_ru_upload_bytes,latest_handshake_at) VALUES(gen_random_uuid(),$1::uuid,$2,$3,$4,$5,$6,$7,$8,$9)`, peer.ID, now, transmitDelta, receiveDelta, trafficDelta.RUDownloadBytes, trafficDelta.RUUploadBytes, trafficDelta.NonRUDownloadBytes, trafficDelta.NonRUUploadBytes, handshake); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Retention can touch millions of metric rows. It must not run while the
	// heartbeat transaction owns the relay row lock, otherwise admin mutations
	// (notably peer deletion) wait behind housekeeping until the HTTP request
	// times out and its transaction rolls back.
	s.runRetentionCleanup(ctx, relayID, now)
	return nil
}

const (
	retentionCleanupInterval = time.Hour
	retentionCleanupTimeout  = 15 * time.Second
)

func (s *Service) runRetentionCleanup(ctx context.Context, relayID string, now time.Time) {
	s.retentionMu.Lock()
	lastCleanup := s.lastRetentionCleanup[relayID]
	if !lastCleanup.IsZero() && now.Sub(lastCleanup) < retentionCleanupInterval {
		s.retentionMu.Unlock()
		return
	}
	s.lastRetentionCleanup[relayID] = now
	s.retentionMu.Unlock()

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), retentionCleanupTimeout)
	defer cancel()
	cutoff := now.Add(-31 * 24 * time.Hour)
	if _, err := s.pool.Exec(cleanupCtx, `DELETE FROM wireguard_peer_metric_samples WHERE recorded_at<$1`, cutoff); err != nil {
		slog.Warn("WireGuard peer metric retention cleanup failed", "relay_id", relayID, "error", err)
		return
	}
	if _, err := s.pool.Exec(cleanupCtx, `DELETE FROM wireguard_exit_health_samples WHERE relay_id=$1::uuid AND recorded_at<$2`, relayID, cutoff); err != nil {
		slog.Warn("WireGuard exit health retention cleanup failed", "relay_id", relayID, "error", err)
	}
}

func peerMutationError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "55P03" {
		return conflict("WireGuard is updating; try again")
	}
	return err
}

func (s *Service) Metrics(ctx context.Context, relayID, peerID, rangeName string) (Metrics, error) {
	if _, err := s.relay(ctx, relayID); err != nil {
		return Metrics{}, err
	}
	if _, err := s.peer(ctx, relayID, peerID); err != nil {
		return Metrics{}, err
	}
	rangeName, from, to, bucket, err := metricRange(rangeName, s.now())
	if err != nil {
		return Metrics{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT recorded_at,download_bytes,upload_bytes,ru_download_bytes,ru_upload_bytes,non_ru_download_bytes,non_ru_upload_bytes FROM wireguard_peer_metric_samples WHERE peer_id=$1::uuid AND recorded_at>=$2 AND recorded_at<$3 ORDER BY recorded_at`, peerID, from, to)
	if err != nil {
		return Metrics{}, err
	}
	defer rows.Close()
	points := map[int64]MetricPoint{}
	for rows.Next() {
		var at time.Time
		var d, u, rd, ru, nd, nu int64
		if err := rows.Scan(&at, &d, &u, &rd, &ru, &nd, &nu); err != nil {
			return Metrics{}, err
		}
		index := at.Sub(from).Milliseconds() / bucket.Milliseconds()
		p := points[index]
		p.BucketStart = from.Add(time.Duration(index) * bucket)
		p.DownloadBytes += d
		p.UploadBytes += u
		p.RUDownloadBytes += rd
		p.RUUploadBytes += ru
		p.NonRUDownloadBytes += nd
		p.NonRUUploadBytes += nu
		points[index] = p
	}
	keys := make([]int64, 0, len(points))
	for key := range points {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	result := Metrics{PeerID: peerID, Range: rangeName, From: from, To: to, Points: []MetricPoint{}}
	for _, key := range keys {
		point := points[key]
		result.Points = append(result.Points, point)
		result.Summary.DownloadBytes += point.DownloadBytes
		result.Summary.UploadBytes += point.UploadBytes
		result.Summary.RUDownloadBytes += point.RUDownloadBytes
		result.Summary.RUUploadBytes += point.RUUploadBytes
		result.Summary.NonRUDownloadBytes += point.NonRUDownloadBytes
		result.Summary.NonRUUploadBytes += point.NonRUUploadBytes
	}
	return result, rows.Err()
}

func (s *Service) Snapshot(ctx context.Context, relayID, rangeName string) (Snapshot, error) {
	relay, err := s.relay(ctx, relayID)
	if err != nil {
		return Snapshot{}, err
	}
	categories, err := listPeerCategories(ctx, s.pool, relayID)
	if err != nil {
		return Snapshot{}, err
	}
	rangeName, from, to, bucket, err := metricRange(rangeName, s.now())
	if err != nil {
		return Snapshot{}, err
	}

	peerRows, err := s.pool.Query(ctx, `SELECT `+peerColumns+` FROM wireguard_peers WHERE relay_id=$1::uuid ORDER BY sort_order ASC,created_at ASC`, relayID)
	if err != nil {
		return Snapshot{}, err
	}
	peers := make([]Peer, 0)
	for peerRows.Next() {
		value, scanErr := scanPeer(peerRows)
		if scanErr != nil {
			peerRows.Close()
			return Snapshot{}, scanErr
		}
		peers = append(peers, value.Peer)
	}
	if err := peerRows.Err(); err != nil {
		peerRows.Close()
		return Snapshot{}, err
	}
	peerRows.Close()

	metricsByPeer := make(map[string]Metrics, len(peers))
	pointsByPeer := make(map[string]map[int64]MetricPoint, len(peers))
	for _, peer := range peers {
		metricsByPeer[peer.ID] = Metrics{PeerID: peer.ID, Range: rangeName, From: from, To: to, Points: []MetricPoint{}}
		pointsByPeer[peer.ID] = map[int64]MetricPoint{}
	}

	metricRows, err := s.pool.Query(ctx, `SELECT p.id::text,m.recorded_at,m.download_bytes,m.upload_bytes,m.ru_download_bytes,m.ru_upload_bytes,m.non_ru_download_bytes,m.non_ru_upload_bytes FROM wireguard_peer_metric_samples m JOIN wireguard_peers p ON p.id=m.peer_id WHERE p.relay_id=$1::uuid AND m.recorded_at>=$2 AND m.recorded_at<$3 ORDER BY p.id,m.recorded_at`, relayID, from, to)
	if err != nil {
		return Snapshot{}, err
	}
	for metricRows.Next() {
		var peerID string
		var at time.Time
		var download, upload, ruDownload, ruUpload, nonRUDownload, nonRUUpload int64
		if err := metricRows.Scan(&peerID, &at, &download, &upload, &ruDownload, &ruUpload, &nonRUDownload, &nonRUUpload); err != nil {
			metricRows.Close()
			return Snapshot{}, err
		}
		index := at.Sub(from).Milliseconds() / bucket.Milliseconds()
		point := pointsByPeer[peerID][index]
		point.BucketStart = from.Add(time.Duration(index) * bucket)
		point.DownloadBytes += download
		point.UploadBytes += upload
		point.RUDownloadBytes += ruDownload
		point.RUUploadBytes += ruUpload
		point.NonRUDownloadBytes += nonRUDownload
		point.NonRUUploadBytes += nonRUUpload
		pointsByPeer[peerID][index] = point
	}
	if err := metricRows.Err(); err != nil {
		metricRows.Close()
		return Snapshot{}, err
	}
	metricRows.Close()

	for peerIndex := range peers {
		peerID := peers[peerIndex].ID
		metrics := metricsByPeer[peerID]
		keys := make([]int64, 0, len(pointsByPeer[peerID]))
		for key := range pointsByPeer[peerID] {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		for _, key := range keys {
			point := pointsByPeer[peerID][key]
			metrics.Points = append(metrics.Points, point)
			metrics.Summary.DownloadBytes += point.DownloadBytes
			metrics.Summary.UploadBytes += point.UploadBytes
			metrics.Summary.RUDownloadBytes += point.RUDownloadBytes
			metrics.Summary.RUUploadBytes += point.RUUploadBytes
			metrics.Summary.NonRUDownloadBytes += point.NonRUDownloadBytes
			metrics.Summary.NonRUUploadBytes += point.NonRUUploadBytes
		}
		metricsByPeer[peerID] = metrics
		peers[peerIndex].Traffic = PeriodTraffic{TrafficTotals: metrics.Summary, Range: rangeName, From: from, To: to}
	}

	exitHistory, err := s.exitHealthHistory(ctx, relayID, rangeName, from, to, bucket)
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{Relay: relay.Relay, Categories: categories, Peers: peers, PeerMetrics: metricsByPeer, ExitHealthHistory: exitHistory}, nil
}

type exitHealthBucket struct {
	point                   ExitHealthMetricPoint
	primaryHealthy          int
	secondaryHealthy        int
	primaryLatencyTotal     float64
	secondaryLatencyTotal   float64
	primaryLatencySamples   int
	secondaryLatencySamples int
}

func (s *Service) exitHealthHistory(ctx context.Context, relayID, rangeName string, from, to time.Time, bucket time.Duration) (ExitHealthHistory, error) {
	rows, err := s.pool.Query(ctx, `SELECT recorded_at,overall_status,active_exit,primary_healthy,primary_latency_ms,primary_failure_reason,secondary_healthy,secondary_latency_ms,secondary_failure_reason FROM wireguard_exit_health_samples WHERE relay_id=$1::uuid AND recorded_at>=$2 AND recorded_at<=$3 ORDER BY recorded_at`, relayID, from, to)
	if err != nil {
		return ExitHealthHistory{}, err
	}
	defer rows.Close()
	buckets := map[int64]*exitHealthBucket{}
	for rows.Next() {
		var at time.Time
		var overallStatus string
		var activeExit, primaryReason, secondaryReason *string
		var primaryHealthy, secondaryHealthy bool
		var primaryLatency, secondaryLatency *float64
		if err := rows.Scan(&at, &overallStatus, &activeExit, &primaryHealthy, &primaryLatency, &primaryReason, &secondaryHealthy, &secondaryLatency, &secondaryReason); err != nil {
			return ExitHealthHistory{}, err
		}
		index := at.Sub(from).Milliseconds() / bucket.Milliseconds()
		value := buckets[index]
		if value == nil {
			value = &exitHealthBucket{point: ExitHealthMetricPoint{BucketStart: from.Add(time.Duration(index) * bucket)}}
			buckets[index] = value
		}
		value.point.Samples++
		if primaryHealthy {
			value.primaryHealthy++
		}
		if secondaryHealthy {
			value.secondaryHealthy++
		}
		if primaryLatency != nil {
			value.primaryLatencyTotal += *primaryLatency
			value.primaryLatencySamples++
		}
		if secondaryLatency != nil {
			value.secondaryLatencyTotal += *secondaryLatency
			value.secondaryLatencySamples++
		}
		if primaryReason != nil {
			value.point.PrimaryFailureReason = primaryReason
		}
		if secondaryReason != nil {
			value.point.SecondaryFailureReason = secondaryReason
		}
		value.point.ActiveExit = activeExit
		value.point.OverallStatus = overallStatus
	}
	if err := rows.Err(); err != nil {
		return ExitHealthHistory{}, err
	}

	keys := make([]int64, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	result := ExitHealthHistory{Range: rangeName, From: from, To: to, Points: make([]ExitHealthMetricPoint, 0, len(keys))}
	for _, key := range keys {
		value := buckets[key]
		value.point.PrimaryAvailabilityPercent = float64(value.primaryHealthy) * 100 / float64(value.point.Samples)
		value.point.SecondaryAvailabilityPercent = float64(value.secondaryHealthy) * 100 / float64(value.point.Samples)
		if value.primaryLatencySamples > 0 {
			average := value.primaryLatencyTotal / float64(value.primaryLatencySamples)
			value.point.PrimaryAverageLatencyMs = &average
		}
		if value.secondaryLatencySamples > 0 {
			average := value.secondaryLatencyTotal / float64(value.secondaryLatencySamples)
			value.point.SecondaryAverageLatencyMs = &average
		}
		result.Points = append(result.Points, value.point)
	}
	return result, nil
}

func metricRange(rangeName string, to time.Time) (string, time.Time, time.Time, time.Duration, error) {
	if rangeName == "" {
		rangeName = "HOUR"
	}
	var window, bucket time.Duration
	switch rangeName {
	case "HOUR":
		window, bucket = time.Hour, time.Minute
	case "DAY":
		window, bucket = 24*time.Hour, 15*time.Minute
	case "WEEK":
		window, bucket = 7*24*time.Hour, time.Hour
	case "MONTH":
		window, bucket = 30*24*time.Hour, 6*time.Hour
	default:
		return "", time.Time{}, time.Time{}, 0, badRequest("Invalid range")
	}
	return rangeName, to.Add(-window), to, bucket, nil
}

func (s *Service) peerTrafficTotals(ctx context.Context, relayID string, from, to time.Time) (map[string]TrafficTotals, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id::text,COALESCE(SUM(m.download_bytes),0),COALESCE(SUM(m.upload_bytes),0),COALESCE(SUM(m.ru_download_bytes),0),COALESCE(SUM(m.ru_upload_bytes),0),COALESCE(SUM(m.non_ru_download_bytes),0),COALESCE(SUM(m.non_ru_upload_bytes),0) FROM wireguard_peers p LEFT JOIN wireguard_peer_metric_samples m ON m.peer_id=p.id AND m.recorded_at>=$2 AND m.recorded_at<$3 WHERE p.relay_id=$1::uuid GROUP BY p.id`, relayID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]TrafficTotals{}
	for rows.Next() {
		var peerID string
		var totals TrafficTotals
		if err := rows.Scan(&peerID, &totals.DownloadBytes, &totals.UploadBytes, &totals.RUDownloadBytes, &totals.RUUploadBytes, &totals.NonRUDownloadBytes, &totals.NonRUUploadBytes); err != nil {
			return nil, err
		}
		result[peerID] = totals
	}
	return result, rows.Err()
}

func (s *Service) AgentTokenMatches(ctx context.Context, relayID, token string) bool {
	if len(token) < 40 || len(token) > 512 {
		return false
	}
	var expected string
	if err := s.pool.QueryRow(ctx, `SELECT agent_token_hash FROM wireguard_relays WHERE id=$1::uuid`, relayID).Scan(&expected); err != nil {
		return false
	}
	actual := tokenHash(token)
	return len(expected) == len(actual) && subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func (s *Service) relay(ctx context.Context, id string) (relayRecord, error) {
	value, err := scanRelay(s.pool.QueryRow(ctx, `SELECT `+relayColumns+` FROM wireguard_relays WHERE id=$1::uuid`, id), s.now())
	if errors.Is(err, pgx.ErrNoRows) {
		return relayRecord{}, notFound("WireGuard relay not found")
	}
	return value, err
}
func (s *Service) peer(ctx context.Context, relayID, id string) (peerRecord, error) {
	value, err := scanPeer(s.pool.QueryRow(ctx, `SELECT `+peerColumns+` FROM wireguard_peers WHERE id=$1::uuid AND relay_id=$2::uuid`, id, relayID))
	if errors.Is(err, pgx.ErrNoRows) {
		return peerRecord{}, notFound("WireGuard peer not found")
	}
	return value, err
}
func (s *Service) now() time.Time { return s.clock().UTC() }
func relayStatus(relay Relay, now time.Time) string {
	if relay.ServerPublicKey == nil || relay.LastSeenAt == nil {
		return "WAITING_FOR_AGENT"
	}
	if now.Sub(*relay.LastSeenAt) > 2*time.Minute {
		return "STALE"
	}
	if relay.AppliedRevision != nil && *relay.AppliedRevision == relay.DesiredRevision {
		if relay.RoutingHealthy == nil || !*relay.RoutingHealthy || relay.RoutingCheckedAt == nil || relay.RoutingCheckedAt.After(now.Add(5*time.Second)) || now.Sub(*relay.RoutingCheckedAt) > 45*time.Second {
			return "DEGRADED"
		}
		if relay.ExitHealth == nil || relay.ExitHealth.CheckedAt.After(now.Add(5*time.Second)) || now.Sub(relay.ExitHealth.CheckedAt) > 45*time.Second {
			return "DEGRADED"
		}
		switch relay.ExitHealth.OverallStatus {
		case "HEALTHY":
			return "READY"
		case "DOWN":
			return "DOWN"
		default:
			return "DEGRADED"
		}
	}
	return "SYNCING"
}
func requiredText(value, label string, max int) (string, error) {
	v := strings.TrimSpace(value)
	if v == "" || len(v) > max || strings.ContainsAny(v, "\r\n") {
		return "", badRequest(label + " is invalid")
	}
	return v, nil
}
func validateEndpoint(value string) (string, error) {
	v, err := requiredText(value, "Public endpoint", 255)
	if err != nil {
		return "", err
	}
	host, port, err := net.SplitHostPort(v)
	if err != nil || host == "" || strings.ContainsAny(host, " \t") {
		return "", badRequest("Public endpoint must be host:port")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", badRequest("Public endpoint must be host:port")
	}
	return v, nil
}
func validateIPv4(value, label string) (string, error) {
	v := strings.TrimSpace(value)
	address, err := netip.ParseAddr(v)
	if err != nil || !address.Is4() {
		return "", badRequest(label + " must be an IPv4 address")
	}
	return v, nil
}
func validatePublicKey(value string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return "", badRequest("WireGuard public key is invalid")
	}
	return value, nil
}
func validateProbe(value RouteProbe) error {
	if _, err := validateIPv4(value.Target, "Route probe target"); err != nil {
		return err
	}
	if math.IsNaN(value.PacketLossPercent) || math.IsInf(value.PacketLossPercent, 0) || value.PacketLossPercent < 0 || value.PacketLossPercent > 100 {
		return badRequest("Route probe values are invalid")
	}
	if value.AverageRTTMs != nil && (math.IsNaN(*value.AverageRTTMs) || math.IsInf(*value.AverageRTTMs, 0) || *value.AverageRTTMs < 0 || *value.AverageRTTMs > 60000) {
		return badRequest("Route probe values are invalid")
	}
	if value.PacketLossPercent < 100 && value.AverageRTTMs == nil {
		return badRequest("Route probe values are invalid")
	}
	return nil
}

func validateExitHealth(value *ExitHealth, now time.Time) error {
	if value == nil {
		return nil
	}
	if value.SchemaVersion != 1 || value.CheckedAt.IsZero() || value.CheckedAt.After(now.Add(5*time.Minute)) {
		return badRequest("Exit health is invalid")
	}
	if value.OverallStatus != "HEALTHY" && value.OverallStatus != "DEGRADED" && value.OverallStatus != "DOWN" {
		return badRequest("Exit health is invalid")
	}
	if len(value.Exits) != 2 || len(value.Counters) != 2 {
		return badRequest("Exit health is invalid")
	}
	for _, exitID := range []string{"primary", "secondary"} {
		probe, ok := value.Exits[exitID]
		if !ok || probe.ID != exitID || !exitInterfacePattern.MatchString(probe.Interface) {
			return badRequest("Exit health is invalid")
		}
		if _, err := validateIPv4(probe.ExpectedEgressIP, "Expected exit egress"); err != nil {
			return err
		}
		if probe.ObservedEgressIP != nil {
			if _, err := validateIPv4(*probe.ObservedEgressIP, "Observed exit egress"); err != nil {
				return err
			}
		}
		if probe.HandshakeAtEpoch < 0 || probe.HandshakeAgeSeconds != nil && (*probe.HandshakeAgeSeconds < 0 || *probe.HandshakeAgeSeconds > 86400) {
			return badRequest("Exit health is invalid")
		}
		if probe.LatencyMs != nil && (math.IsNaN(*probe.LatencyMs) || math.IsInf(*probe.LatencyMs, 0) || *probe.LatencyMs < 0 || *probe.LatencyMs > 60000) {
			return badRequest("Exit health is invalid")
		}
		if probe.Healthy {
			if probe.Reason != nil || probe.ObservedEgressIP == nil || *probe.ObservedEgressIP != probe.ExpectedEgressIP || probe.HandshakeAtEpoch == 0 || probe.HandshakeAgeSeconds == nil {
				return badRequest("Exit health is invalid")
			}
		} else if probe.Reason == nil {
			return badRequest("Exit health is invalid")
		}
		counter, ok := value.Counters[exitID]
		if !ok || counter.Successes < 0 || counter.Successes > 1000000 || counter.Failures < 0 || counter.Failures > 1000000 || counter.Successes > 0 && counter.Failures > 0 {
			return badRequest("Exit health is invalid")
		}
	}
	if value.OverallStatus == "DOWN" {
		if value.ActiveExit != nil || value.ActiveInterface != nil {
			return badRequest("Exit health is invalid")
		}
		return nil
	}
	if value.ActiveExit == nil || value.ActiveInterface == nil || (*value.ActiveExit != "primary" && *value.ActiveExit != "secondary") {
		return badRequest("Exit health is invalid")
	}
	active := value.Exits[*value.ActiveExit]
	if *value.ActiveInterface != active.Interface {
		return badRequest("Exit health is invalid")
	}
	if value.OverallStatus == "HEALTHY" && (!active.Healthy || !value.Exits["primary"].Healthy || !value.Exits["secondary"].Healthy) {
		return badRequest("Exit health is invalid")
	}
	return nil
}
func generateToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
func tokenHash(token string) string {
	value := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", value[:])
}

var (
	exitInterfacePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)
	slugPattern          = regexp.MustCompile(`[^a-z0-9]+`)
)

func fileSlug(name, id string) string {
	value := strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(value) > 80 {
		value = value[:80]
	}
	if value == "" {
		value = "wireguard-peer-" + id[:min(8, len(id))]
	}
	return value
}
func counterDelta(previous, current int64) int64 {
	if current >= previous {
		return current - previous
	}
	return current
}
func badRequest(message string) error {
	return &workout.Error{Status: http.StatusBadRequest, Message: message}
}
func conflict(message string) error {
	return &workout.Error{Status: http.StatusConflict, Message: message}
}
func notFound(message string) error {
	return &workout.Error{Status: http.StatusNotFound, Message: message}
}
func unavailable(message string) error {
	return &workout.Error{Status: http.StatusServiceUnavailable, Message: message}
}
