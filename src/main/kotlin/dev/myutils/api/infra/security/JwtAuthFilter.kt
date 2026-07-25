package dev.myutils.api.infra.security

import dev.myutils.api.domain.UserRepository
import dev.myutils.api.domain.UserRole
import dev.myutils.api.infra.session.SessionService
import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken
import org.springframework.security.core.authority.SimpleGrantedAuthority
import org.springframework.security.core.context.SecurityContextHolder
import org.springframework.stereotype.Component
import org.springframework.web.filter.OncePerRequestFilter
import java.util.UUID

@Component
class JwtAuthFilter(
	private val jwtService: JwtService,
	private val userRepository: UserRepository,
	private val sessionService: SessionService,
) : OncePerRequestFilter() {
	override fun doFilterInternal(
		request: HttpServletRequest,
		response: HttpServletResponse,
		filterChain: FilterChain,
	) {
		val header = request.getHeader("Authorization")
		if (header != null && header.startsWith("Bearer ")) {
			val token = header.removePrefix("Bearer ").trim()
			val claims = jwtService.parseClaims(token)
			val sessionId = claims?.id
			val userId = claims?.subject?.let { runCatching { UUID.fromString(it) }.getOrNull() }
			val user =
				userId
					?.takeIf { sessionId != null && sessionService.belongsToUser(sessionId, it) }
					?.let { userRepository.findById(it).orElse(null) }
			if (sessionId != null && user != null) {
				val principal =
					AccountPrincipal(
						userId = user.id,
						username = user.username,
						role = user.role,
					)
				val authorities =
					buildList {
						add(SimpleGrantedAuthority("ROLE_USER"))
						if (user.role == UserRole.ADMIN && !user.mustChangePassword) {
							add(SimpleGrantedAuthority("ROLE_ADMIN"))
						}
					}
				val auth =
					UsernamePasswordAuthenticationToken(
						principal,
						null,
						authorities,
					)
				auth.details = SessionPrincipal(sessionId = sessionId)
				SecurityContextHolder.getContext().authentication = auth
			}
		}
		filterChain.doFilter(request, response)
	}
}

data class SessionPrincipal(
	val sessionId: String,
)

data class AccountPrincipal(
	val userId: UUID,
	val username: String,
	val role: UserRole,
)
