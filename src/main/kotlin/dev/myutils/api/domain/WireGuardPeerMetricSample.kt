package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.FetchType
import jakarta.persistence.Id
import jakarta.persistence.JoinColumn
import jakarta.persistence.ManyToOne
import jakarta.persistence.Table
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "wireguard_peer_metric_samples")
class WireGuardPeerMetricSample(
	@Id
	val id: UUID = UUID.randomUUID(),
	@ManyToOne(fetch = FetchType.LAZY, optional = false)
	@JoinColumn(name = "peer_id", nullable = false)
	val peer: WireGuardPeer,
	@Column(name = "recorded_at", nullable = false)
	val recordedAt: Instant,
	@Column(name = "download_bytes", nullable = false)
	val downloadBytes: Long,
	@Column(name = "upload_bytes", nullable = false)
	val uploadBytes: Long,
	@Column(name = "ru_download_bytes", nullable = false)
	val ruDownloadBytes: Long = 0,
	@Column(name = "ru_upload_bytes", nullable = false)
	val ruUploadBytes: Long = 0,
	@Column(name = "non_ru_download_bytes", nullable = false)
	val nonRuDownloadBytes: Long = 0,
	@Column(name = "non_ru_upload_bytes", nullable = false)
	val nonRuUploadBytes: Long = 0,
	@Column(name = "latest_handshake_at")
	val latestHandshakeAt: Instant?,
)
