package dev.myutils.api.agent.langchain

import dev.myutils.api.agent.WorkoutAgentContextBuilder
import dev.myutils.api.agent.WorkoutToolsService
import dev.myutils.api.config.ConditionalOnTelegramBot
import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.util.LogPreview
import dev.langchain4j.memory.chat.MessageWindowChatMemory
import dev.langchain4j.service.AiServices
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component

@Component
@ConditionalOnTelegramBot
class WorkoutLangChain4jAgent(
	private val properties: MyUtilsProperties,
	private val chatModelFactory: LangChain4jChatModelFactory,
	private val memoryStore: RedisChatMemoryStore,
	private val contextBuilder: WorkoutAgentContextBuilder,
	private val toolsService: WorkoutToolsService,
) {
	private val log = LoggerFactory.getLogger(javaClass)

	fun run(
		chatId: Long,
		userMessage: String,
	): String {
		val tools = WorkoutLangChainTools.create(chatId, toolsService, properties)
		val assistant =
			AiServices
				.builder(WorkoutAgentAssistant::class.java)
				.chatModel(chatModelFactory.create())
				.tools(tools)
				.chatMemoryProvider { id ->
					MessageWindowChatMemory
						.builder()
						.id(id)
						.maxMessages(24)
						.chatMemoryStore(memoryStore)
						.build()
				}.maxSequentialToolsInvocations(AppProperties.OPENROUTER_MAX_TOOL_ITERATIONS.get())
				.build()

		val reply =
			assistant.chat(
				chatId,
				userMessage,
				AppProperties.AGENT_SYSTEM_PROMPT.get(),
				contextBuilder.buildSnapshot(),
			)
		log.info("LangChain4j agent chatId={} reply={}", chatId, LogPreview.of(reply))
		return reply.trim().ifEmpty { "Готово." }
	}
}
