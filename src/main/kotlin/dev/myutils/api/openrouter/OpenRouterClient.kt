package dev.myutils.api.openrouter

import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.util.LogPreview
import dev.myutils.api.util.RetryPolicy
import dev.myutils.api.util.measureMillis
import dev.myutils.api.util.retryBlocking
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component
import org.springframework.web.client.RestClient

@Component
@ConditionalOnTelegramBot
class OpenRouterClient(
	properties: MyUtilsProperties,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val client: RestClient = OpenRouterRestClientFactory.create(properties)

	fun chat(request: ChatCompletionRequest): ChatCompletionResponse {
		val lastUser =
			request.messages.lastOrNull { it.role == "user" }?.content
		log.info(
			"OpenRouter request model={} messages={} tools={} lastUser={}",
			request.model,
			request.messages.size,
			request.tools?.size ?: 0,
			LogPreview.of(lastUser, max = 80),
		)
		val (response, ms) =
			measureMillis {
				retryBlocking(
					name = "OpenRouter chat",
					policy = retryPolicy(),
					log = log,
				) {
					client
						.post()
						.uri("/chat/completions")
						.body(request)
						.retrieve()
						.body(ChatCompletionResponse::class.java)
						?: ChatCompletionResponse()
				}
			}
		val assistant = response.choices.firstOrNull()?.message
		val toolNames = assistant?.toolCalls?.joinToString { it.function.name }.orEmpty()
		log.info(
			"OpenRouter response {}ms choices={} tools={} content={}",
			ms,
			response.choices.size,
			toolNames.ifEmpty { "-" },
			LogPreview.of(assistant?.content, max = 80),
		)
		return response
	}

	private fun retryPolicy(): RetryPolicy =
		RetryPolicy(
			maxAttempts = AppProperties.OPENROUTER_RETRY_MAX_ATTEMPTS.get(),
			initialDelayMs = AppProperties.OPENROUTER_RETRY_INITIAL_DELAY_MS.get().toLong(),
		)
}
