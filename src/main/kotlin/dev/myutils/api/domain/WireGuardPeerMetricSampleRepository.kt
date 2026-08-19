package dev.myutils.api.domain

import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.data.jpa.repository.Modifying
import org.springframework.data.jpa.repository.Query
import org.springframework.data.repository.query.Param
import java.time.Instant
import java.util.UUID

interface WireGuardPeerMetricSampleRepository : JpaRepository<WireGuardPeerMetricSample, UUID> {
	fun findAllByPeerIdAndRecordedAtGreaterThanEqualAndRecordedAtLessThanOrderByRecordedAtAsc(
		peerId: UUID,
		from: Instant,
		to: Instant,
	): List<WireGuardPeerMetricSample>

	@Modifying
	@Query("delete from WireGuardPeerMetricSample sample where sample.recordedAt < :cutoff")
	fun deleteRecordedBefore(@Param("cutoff") cutoff: Instant): Int
}
