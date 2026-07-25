package dev.myutils.api.infra.security

import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import org.springframework.http.HttpMethod
import org.springframework.http.HttpStatus
import org.springframework.security.config.annotation.web.builders.HttpSecurity
import org.springframework.security.web.authentication.HttpStatusEntryPoint
import org.springframework.security.config.annotation.web.configuration.EnableWebSecurity
import org.springframework.security.config.http.SessionCreationPolicy
import org.springframework.security.web.SecurityFilterChain
import org.springframework.security.web.authentication.UsernamePasswordAuthenticationFilter
import org.springframework.security.web.util.matcher.AntPathRequestMatcher

@Configuration
@EnableWebSecurity
class SecurityConfig(
	private val jwtAuthFilter: JwtAuthFilter,
) {
	@Bean
	fun securityFilterChain(http: HttpSecurity): SecurityFilterChain =
		http
			.csrf { it.disable() }
			.cors { }
			.sessionManagement { it.sessionCreationPolicy(SessionCreationPolicy.STATELESS) }
			.exceptionHandling { it.authenticationEntryPoint(HttpStatusEntryPoint(HttpStatus.UNAUTHORIZED)) }
			.authorizeHttpRequests { auth ->
				auth
					.requestMatchers(HttpMethod.GET, "/api/health").permitAll()
					.requestMatchers(HttpMethod.GET, "/api/health/steps").permitAll()
					.requestMatchers(HttpMethod.POST, "/api/health/steps").permitAll()
					.requestMatchers(HttpMethod.GET, "/api/health/weight").permitAll()
					.requestMatchers(HttpMethod.POST, "/api/health/weight").permitAll()
					.requestMatchers(HttpMethod.GET, "/actuator/health").permitAll()
					.requestMatchers(HttpMethod.GET, "/actuator/prometheus").permitAll()
					.requestMatchers(HttpMethod.POST, "/api/auth/login").permitAll()
					.requestMatchers(HttpMethod.POST, "/api/auth/register").permitAll()
					.requestMatchers("/api/workouts/**").permitAll()
					.requestMatchers(
						AntPathRequestMatcher("/api/admin/settings"),
						AntPathRequestMatcher("/api/admin/settings/**"),
						AntPathRequestMatcher("/api/admin/agent-memory"),
						AntPathRequestMatcher("/api/admin/agent-memory/**"),
					).hasRole("ADMIN")
					.requestMatchers("/api/admin/**").hasRole("ADMIN")
					.requestMatchers("/api/auth/**").authenticated()
					.anyRequest().denyAll()
			}
			.addFilterBefore(jwtAuthFilter, UsernamePasswordAuthenticationFilter::class.java)
			.build()
}
