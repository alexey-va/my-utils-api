package dev.myutils.api.domain

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import org.hibernate.annotations.JdbcTypeCode
import org.hibernate.type.SqlTypes
import java.time.Instant

@Entity
@Table(name = "app_settings")
class AppSetting(
	@Id
	@Column(length = 128)
	val key: String,
	@JdbcTypeCode(SqlTypes.JSON)
	@Column(columnDefinition = "jsonb", nullable = false)
	var value: String,
	@Column(name = "updated_at", nullable = false)
	var updatedAt: Instant = Instant.now(),
	@Column(name = "updated_by")
	var updatedBy: String? = null,
)
