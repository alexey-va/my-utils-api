package dev.myutils.api.agent

import dev.myutils.api.agent.langchain.WorkoutLangChain4jAgent
import dev.myutils.api.infra.config.ConditionalOnTelegramBot
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.properties.AppProperties
import dev.myutils.api.temporal.TemporalWorkflowService
import dev.myutils.api.temporal.agent.AgentTurnInput
import dev.myutils.api.telegram.AgentStatusMessenger
import dev.myutils.api.telegram.TelegramMessenger
import dev.myutils.api.infra.observability.AgentMetrics
import dev.myutils.api.infra.observability.GenAiTracing
import dev.myutils.api.infra.util.LogPreview
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.ObjectProvider
import org.springframework.stereotype.Service

@Service
@ConditionalOnTelegramBot
class WorkoutAgentService(
	private val properties: MyUtilsProperties,
	private val langChain4jAgent: WorkoutLangChain4jAgent,
	private val temporalWorkflow: ObjectProvider<TemporalWorkflowService>,
	private val telegram: TelegramMessenger,
	private val agentStatus: AgentStatusMessenger,
	private val agentMetrics: AgentMetrics,
	private val genAiTracing: GenAiTracing,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	suspend fun handleMessage(
		chatId: Long,
		userId: Long,
		text: String,
	) {
		if (properties.telegram.allowedUserIdSet().isNotEmpty() && userId !in properties.telegram.allowedUserIdSet()) {
			log.warn("Rejected Telegram user {}", userId)
			agentMetrics.recordRequest("none", "rejected")
			telegram.sendHtmlMessage(chatId, "У вас нет доступа к этому боту.")
			return
		}

		log.info(
			"Telegram inbound chatId={} userId={} text={}",
			chatId,
			userId,
			LogPreview.of(text),
		)

		val temporal = temporalWorkflow.getIfAvailable()
		if (temporal != null && properties.temporal.enabled) {
			agentMetrics.recordReceived("temporal")
			if (text != "/start") {
				agentStatus.begin(chatId)
			}
			genAiTracing.invokeAgent(chatId, userId, text) {
				val traceParent = genAiTracing.currentTraceParent()
				temporal.startAgentTurn(
					AgentTurnInput(
						chatId = chatId,
						userId = userId,
						text = text,
						maxToolIterations = AppProperties.OPENROUTER_MAX_TOOL_ITERATIONS.get(),
						traceParent = traceParent,
					),
				)
			}
			log.info("Telegram chatId={} delegated to Temporal agent workflow", chatId)
			return
		}

		runDirect(chatId, userId, text)
	}

	private suspend fun runDirect(
		chatId: Long,
		userId: Long,
		text: String,
	) {
		if (text == "/start") {
			log.info("Telegram /start chatId={}", chatId)
			agentMetrics.recordReceived("direct")
			agentMetrics.recordInbound("direct", "start_command", durationMs = 0, llmSteps = 0)
			telegram.sendHtmlMessage(
				chatId,
				"""
				Тренер по дневнику. Напиши «что на сегодня» — скажу что уже было на этой неделе, что осталось по списку упражнений, и один план с весами. Или сразу запиши подход: «жим 70 3*10/12».
				""".trimIndent(),
			)
			return
		}

		agentMetrics.recordReceived("direct")
		agentStatus.begin(chatId)
		val startedAt = System.currentTimeMillis()
		try {
			val reply =
				genAiTracing.invokeAgent(chatId, userId, text) {
					langChain4jAgent.run(chatId, text)
				}
			agentStatus.complete(chatId)
			telegram.sendHtmlMessage(chatId, reply)
			agentMetrics.recordInbound(
				path = "direct",
				outcome = "reply",
				durationMs = System.currentTimeMillis() - startedAt,
				llmSteps = 0,
			)
			log.info("Telegram handled chatId={} reply={}", chatId, LogPreview.of(reply))
		} catch (ex: Exception) {
			log.error("Direct agent failed chatId={}: {}", chatId, ex.message, ex)
			agentStatus.fail(chatId, "❌ Не удалось обработать запрос. Попробуй ещё раз.")
			agentMetrics.recordInbound(
				path = "direct",
				outcome = "error",
				durationMs = System.currentTimeMillis() - startedAt,
				llmSteps = 0,
			)
		}
	}
}
