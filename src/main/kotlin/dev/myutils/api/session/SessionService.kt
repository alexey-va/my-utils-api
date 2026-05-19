package dev.myutils.api.session

import dev.myutils.api.config.MyUtilsProperties
import org.springframework.data.redis.core.StringRedisTemplate
import org.springframework.stereotype.Service
import java.time.Duration

@Service
class SessionService(
    private val redis: StringRedisTemplate,
    private val properties: MyUtilsProperties,
) {
    private fun key(sessionId: String): String = "${properties.session.redisKeyPrefix}$sessionId"

    fun store(
        sessionId: String,
        email: String,
        ttl: Duration,
    ) {
        redis.opsForValue().set(key(sessionId), email, ttl)
    }

    fun exists(sessionId: String): Boolean = redis.hasKey(key(sessionId)) == true

    fun revoke(sessionId: String) {
        redis.delete(key(sessionId))
    }
}
