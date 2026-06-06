package dev.myutils.api.infra.security

import dev.myutils.api.service.AuthService
import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken
import org.springframework.security.core.authority.SimpleGrantedAuthority
import org.springframework.security.core.context.SecurityContextHolder
import org.springframework.stereotype.Component
import org.springframework.web.filter.OncePerRequestFilter

@Component
class JwtAuthFilter(
	private val jwtService: JwtService,
	private val authService: AuthService,
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
			val email = claims?.subject
			if (sessionId != null && email != null && authService.validateSession(sessionId)) {
				val auth =
					UsernamePasswordAuthenticationToken(
						email,
						null,
						listOf(SimpleGrantedAuthority("ROLE_USER")),
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
