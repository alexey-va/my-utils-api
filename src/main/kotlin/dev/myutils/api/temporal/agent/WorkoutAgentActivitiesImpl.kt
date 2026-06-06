package dev.myutils.api.temporal.agent

import dev.myutils.api.agent.langchain.WorkoutLangChain4jAgent
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.temporal.TemporalConstants
import io.temporal.spring.boot.ActivityImpl
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.ObjectProvider
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.stereotype.Component

@Component
@ConditionalOnProperty(prefix = "myutils.temporal", name = ["enabled"], havingValue = "true")
@ActivityImpl(taskQueues = [TemporalConstants.TASK_QUEUE])
class WorkoutAgentActivitiesImpl(
	private val agent: ObjectProvider<WorkoutLangChain4jAgent>,
	private val properties: MyUtilsProperties,
) : WorkoutAgentActivities {
	private val log = LoggerFactory.getLogger(javaClass)

	override fun resolvePrelude(input: AgentTurnInput): AgentPreludeResult {
		val allowed = properties.telegram.allowedUserIdSet()
		if (allowed.isNotEmpty() && input.userId !in allowed) {
			log.warn("Rejected Telegram user {}", input.userId)
			return AgentPreludeResult(AgentPreludeResult.Kind.REPLY, "У вас нет доступа к этому боту.")
		}
		if (input.text == "/start") {
			return AgentPreludeResult(
				AgentPreludeResult.Kind.REPLY,
				"""
				Тренер по дневнику. Напиши «что на сегодня» — скажу что уже было на этой неделе, что осталось по списку упражнений, и один план с весами. Или сразу запиши подход: «жим 70 3*10/12».
				""".trimIndent(),
			)
		}
		return AgentPreludeResult(AgentPreludeResult.Kind.CONTINUE)
	}

	override fun llmStep(input: AgentLlmStepInput): AgentLlmStepResult {
		val langChainAgent =
			agent.getIfAvailable()
				?: return AgentLlmStepResult(reply = "Агент не настроен (нет TELEGRAM_BOT_TOKEN?).")
		return langChainAgent.llmStep(input)
	}

	override fun recordToolResults(input: RecordToolResultsInput) {
		val langChainAgent =
			agent.getIfAvailable()
				?: return
		langChainAgent.recordToolResults(input)
	}
}
