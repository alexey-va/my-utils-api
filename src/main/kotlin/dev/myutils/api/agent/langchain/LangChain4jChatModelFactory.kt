package dev.myutils.api.agent.langchain

import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import dev.langchain4j.http.client.jdk.JdkHttpClient
import dev.langchain4j.model.chat.ChatModel
import dev.langchain4j.model.openai.OpenAiChatModel
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component
import java.net.InetSocketAddress
import java.net.ProxySelector
import java.net.http.HttpClient
import java.time.Duration

fun interface ChatModelFactory {
	fun create(): ChatModel
}

@Component
@ConditionalOnTelegramBot
class LangChain4jChatModelFactory(
	private val properties: MyUtilsProperties,
) : ChatModelFactory {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun create(): ChatModel {
		val config = properties.openrouter
		val jdkBuilder =
			HttpClient
				.newBuilder()
				.connectTimeout(Duration.ofSeconds(30))
		val proxy = config.proxy
		if (proxy.enabled && proxy.host.isNotBlank()) {
			jdkBuilder.proxy(
				ProxySelector.of(InetSocketAddress(proxy.host.trim(), proxy.port)),
			)
			log.info("LangChain4j HTTP via proxy {}:{}", proxy.host, proxy.port)
		}
		val httpClientBuilder = JdkHttpClient.builder().httpClientBuilder(jdkBuilder)
		return OpenAiChatModel
			.builder()
			.baseUrl(config.baseUrl.trimEnd('/'))
			.apiKey(config.apiKey)
			.modelName(AppProperties.OPENROUTER_MODEL.get())
			.customHeaders(
				mapOf(
					"HTTP-Referer" to config.httpReferer,
					"X-Title" to config.appTitle,
				),
			)
			.timeout(Duration.ofMinutes(3))
			.httpClientBuilder(httpClientBuilder)
			.logRequests(true)
			.logResponses(true)
			.build()
	}
}
