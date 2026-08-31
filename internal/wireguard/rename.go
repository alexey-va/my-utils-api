package wireguard

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// RenamePeerForVPNBot renames an approved user's owned peer and records the
// mutation in the same transaction. Names are metadata, so this does not bump
// the relay's desired revision.
func (s *Service) RenamePeerForVPNBot(ctx context.Context, relayID, peerID string, telegramUserID int64, requestedName string) (Peer, error) {
	if telegramUserID <= 0 {
		return Peer{}, badRequest("Telegram user ID is invalid")
	}
	name, err := requiredText(requestedName, "Peer name", 120)
	if err != nil {
		return Peer{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Peer{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := lockApprovedVPNBotOwnerTx(ctx, tx, telegramUserID, peerID); err != nil {
		return Peer{}, err
	}
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
	if peer.Name == name {
		if err := tx.Commit(ctx); err != nil {
			return Peer{}, err
		}
		return peer.Peer, nil
	}
	var duplicate bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wireguard_peers WHERE relay_id=$1::uuid AND id<>$2::uuid AND lower(name)=lower($3))`, relayID, peerID, name).Scan(&duplicate); err != nil {
		return Peer{}, err
	}
	if duplicate {
		return Peer{}, conflict("Peer name already exists")
	}
	peer, err = scanPeer(tx.QueryRow(ctx, `UPDATE wireguard_peers SET name=$3,updated_at=$4 WHERE id=$1::uuid AND relay_id=$2::uuid RETURNING `+peerColumns, peerID, relayID, name, s.now()))
	if err != nil {
		return Peer{}, err
	}
	if err := recordVPNBotAuditTx(ctx, tx, telegramUserID, "TUNNEL_RENAMED", peerID); err != nil {
		return Peer{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Peer{}, err
	}
	return peer.Peer, nil
}
