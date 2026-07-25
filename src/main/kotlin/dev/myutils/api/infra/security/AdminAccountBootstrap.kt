package dev.myutils.api.infra.security

import dev.myutils.api.domain.User
import dev.myutils.api.domain.UserRepository
import dev.myutils.api.domain.UserRole
import dev.myutils.api.infra.config.MyUtilsProperties
import org.slf4j.LoggerFactory
import org.springframework.boot.ApplicationArguments
import org.springframework.boot.ApplicationRunner
import org.springframework.security.crypto.password.PasswordEncoder
import org.springframework.stereotype.Component

@Component
class AdminAccountBootstrap(
	private val userRepository: UserRepository,
	private val passwordEncoder: PasswordEncoder,
	private val properties: MyUtilsProperties,
) : ApplicationRunner {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun run(args: ApplicationArguments) {
		val config = properties.auth.bootstrapAdmin
		if (!config.enabled || userRepository.existsByRole(UserRole.ADMIN)) {
			return
		}

		val username = config.username.trim()
		val email = config.email.trim().lowercase()
		check(username.isNotEmpty()) { "Bootstrap admin username must not be blank" }
		check(config.password.isNotEmpty()) { "Bootstrap admin password must not be blank" }

		val existing =
			userRepository
				.findFirstByUsernameIgnoreCaseOrEmailIgnoreCase(username, email)
				.orElse(null)

		val admin =
			if (existing == null) {
				User(
					username = username,
					email = email,
					passwordHash = passwordEncoder.encode(config.password),
					role = UserRole.ADMIN,
					mustChangePassword = true,
				)
			} else {
				existing.apply {
					this.username = username
					this.email = email
					this.passwordHash = passwordEncoder.encode(config.password)
					this.role = UserRole.ADMIN
					this.mustChangePassword = true
				}
			}

		userRepository.save(admin)
		log.warn(
			"Bootstrap admin account created username={}; change its password immediately",
			username,
		)
	}
}
