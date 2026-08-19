package dev.myutils.api.infra.security

import dev.myutils.api.service.WireGuardControlPlaneService
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
class WireGuardAgentAuthFilter(
	private val wireGuard: WireGuardControlPlaneService,
) : OncePerRequestFilter() {
	override fun shouldNotFilter(request: HttpServletRequest): Boolean =
		!request.requestURI.startsWith(INTERNAL_PREFIX)

	override fun doFilterInternal(
		request: HttpServletRequest,
		response: HttpServletResponse,
		filterChain: FilterChain,
	) {
		val match = PATH.matchEntire(request.requestURI)
		val relayId = match?.groupValues?.get(1)?.let { runCatching { UUID.fromString(it) }.getOrNull() }
		val token = request.getHeader(TOKEN_HEADER)?.trim().orEmpty()
		if (relayId == null || !wireGuard.agentTokenMatches(relayId, token)) {
			response.sendError(HttpServletResponse.SC_UNAUTHORIZED)
			return
		}
		SecurityContextHolder.getContext().authentication =
			UsernamePasswordAuthenticationToken(
				WireGuardAgentPrincipal(relayId),
				null,
				listOf(SimpleGrantedAuthority("ROLE_WIREGUARD_AGENT")),
			)
		filterChain.doFilter(request, response)
	}

	private companion object {
		const val INTERNAL_PREFIX = "/api/internal/wireguard/"
		const val TOKEN_HEADER = "X-WireGuard-Agent-Token"
		val PATH = Regex("^/api/internal/wireguard/relays/([0-9a-fA-F-]{36})/(?:desired|heartbeat)$")
	}
}

data class WireGuardAgentPrincipal(
	val relayId: UUID,
)
