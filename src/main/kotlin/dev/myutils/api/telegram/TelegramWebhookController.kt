package dev.myutils.api.telegram

import dev.myutils.api.agent.WorkoutAgentService
import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import org.slf4j.LoggerFactory
import org.springframework.http.ResponseEntity
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController

@RestController
@RequestMapping("/api/telegram")
@ConditionalOnTelegramBot
class TelegramWebhookController(
	private val properties: MyUtilsProperties,
	private val workoutAgentService: WorkoutAgentService,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	@PostMapping("/webhook/{secret}")
	fun webhook(
		@PathVariable secret: String,
		@RequestBody update: TelegramUpdate,
	): ResponseEntity<Void> {
		if (secret != properties.telegram.webhookSecret) {
			log.warn("Telegram webhook rejected: bad secret")
			return ResponseEntity.notFound().build()
		}
		log.info("Telegram webhook updateId={}", update.updateId)
		workoutAgentService.handleUpdateAsync(update)
		return ResponseEntity.ok().build()
	}
}
