package dev.myutils.api.infra.session

import dev.myutils.api.infra.config.MyUtilsProperties
import org.springframework.data.redis.core.StringRedisTemplate
import org.springframework.stereotype.Service
import java.time.Duration
import java.util.UUID

@Service
class SessionService(
    private val redis: StringRedisTemplate,
    private val properties: MyUtilsProperties,
) {
    private fun key(sessionId: String): String = "${properties.session.redisKeyPrefix}$sessionId"

    fun store(
        sessionId: String,
        userId: UUID,
        ttl: Duration,
    ) {
        val userIdText = userId.toString()
        redis.opsForValue().set(key(sessionId), userIdText, ttl)
        val userSessionsKey = userSessionsKey(userId)
        redis.opsForSet().add(userSessionsKey, sessionId)
        redis.expire(userSessionsKey, ttl)
    }

    fun belongsToUser(
        sessionId: String,
        userId: UUID,
    ): Boolean = redis.opsForValue().get(key(sessionId)) == userId.toString()

    fun revoke(sessionId: String) {
        redis.delete(key(sessionId))
    }

    fun revokeUserSessions(userId: UUID) {
        val userSessionsKey = userSessionsKey(userId)
        val sessionIds = redis.opsForSet().members(userSessionsKey).orEmpty()
        if (sessionIds.isNotEmpty()) {
            redis.delete(sessionIds.map(::key))
        }
        redis.delete(userSessionsKey)
    }

    private fun userSessionsKey(userId: UUID): String =
        "${properties.session.userSessionsKeyPrefix}$userId"
}
