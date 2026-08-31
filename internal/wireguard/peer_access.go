package wireguard

import (
	"context"
	"fmt"
)

// SetPeerIDsEnabled applies an account-level access decision atomically and
// advances the relay desired revision once.
func (s *Service) SetPeerIDsEnabled(ctx context.Context, relayID string, peerIDs []string, enabled bool) error {
	if len(peerIDs) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if enabled {
		rows, err := tx.Query(ctx, `
			SELECT bot_user.status
			FROM wireguard_vpn_bot_peer_owners owner
			JOIN wireguard_vpn_bot_users bot_user ON bot_user.telegram_user_id=owner.telegram_user_id
			WHERE owner.peer_id=ANY($1::uuid[])
			ORDER BY bot_user.telegram_user_id
			FOR UPDATE OF bot_user`, peerIDs)
		if err != nil {
			return err
		}
		for rows.Next() {
			var status string
			if err := rows.Scan(&status); err != nil {
				rows.Close()
				return err
			}
			if status != "APPROVED" {
				rows.Close()
				return conflict("A VPN bot peer cannot be enabled while its owner is not approved")
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	now := s.now()
	result, err := tx.Exec(ctx, `UPDATE wireguard_relays SET desired_revision=desired_revision+1,updated_at=$2 WHERE id=$1::uuid`, relayID, now)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return notFound("WireGuard relay not found")
	}
	result, err = tx.Exec(ctx, `UPDATE wireguard_peers SET enabled=$3,updated_at=$4 WHERE relay_id=$1::uuid AND id=ANY($2::uuid[])`, relayID, peerIDs, enabled, now)
	if err != nil {
		return err
	}
	if result.RowsAffected() != int64(len(peerIDs)) {
		return fmt.Errorf("WireGuard peer ownership set changed during access update")
	}
	return tx.Commit(ctx)
}
