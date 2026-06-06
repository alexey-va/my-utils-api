package dev.myutils.api.temporal.agent

import io.temporal.activity.ActivityInterface
import io.temporal.activity.ActivityMethod

@ActivityInterface
interface WorkoutAgentActivities {
	@ActivityMethod
	fun resolvePrelude(input: AgentTurnInput): AgentPreludeResult

	@ActivityMethod
	fun llmStep(input: AgentLlmStepInput): AgentLlmStepResult

	@ActivityMethod
	fun recordToolResults(input: RecordToolResultsInput)
}
