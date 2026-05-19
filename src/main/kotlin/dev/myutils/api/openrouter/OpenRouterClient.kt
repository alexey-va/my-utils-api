package dev.myutils.api.openrouter

import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.util.LogPreview
import org.slf4j.LoggerFactory
import org.springframework.http.HttpHeaders
import org.springframework.http.MediaType
import org.springframework.stereotype.Component
import org.springframework.web.client.RestClient

@Component
@ConditionalOnTelegramBot
class OpenRouterClient(
	properties: MyUtilsProperties,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val config = properties.openrouter

	private val client: RestClient =
		RestClient
			.builder()
			.baseUrl(config.baseUrl.trimEnd('/'))
			.defaultHeader(HttpHeaders.AUTHORIZATION, "Bearer ${config.apiKey}")
			.defaultHeader(HttpHeaders.CONTENT_TYPE, MediaType.APPLICATION_JSON_VALUE)
			.defaultHeader("HTTP-Referer", config.httpReferer)
			.defaultHeader("X-Title", config.appTitle)
			.build()

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
		val started = System.nanoTime()
		val response =
			client
				.post()
				.uri("/chat/completions")
				.body(request)
				.retrieve()
				.body(ChatCompletionResponse::class.java)
				?: ChatCompletionResponse()
		val ms = (System.nanoTime() - started) / 1_000_000
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
}
