package wireguard

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ReissuePeerCredentials atomically rotates a peer keypair without changing
// its address, identity or ownership. The old config stops working as soon as
// the relay applies the incremented desired revision.
func (s *Service) ReissuePeerCredentials(ctx context.Context, relayID, peerID string) (PeerCredentials, error) {
	if s.cipher == nil || !s.cipher.Configured() {
		return PeerCredentials{}, unavailable("WireGuard credential encryption is not configured")
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
	peer, err := scanPeer(tx.QueryRow(ctx, `SELECT `+peerColumns+` FROM wireguard_peers WHERE id=$1::uuid AND relay_id=$2::uuid FOR UPDATE`, peerID, relayID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PeerCredentials{}, notFound("WireGuard peer not found")
	}
	if err != nil {
		return PeerCredentials{}, err
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
	peer, err = scanPeer(tx.QueryRow(ctx, `UPDATE wireguard_peers SET public_key=$3,private_key_ciphertext=$4,private_key_nonce=$5,updated_at=$6 WHERE id=$1::uuid AND relay_id=$2::uuid RETURNING `+peerColumns, peerID, relayID, pair.PublicKey, encrypted.Ciphertext, encrypted.Nonce, now))
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
