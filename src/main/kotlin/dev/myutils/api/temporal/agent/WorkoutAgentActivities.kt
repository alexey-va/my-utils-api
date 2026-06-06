package dev.myutils.api.temporal.agent

import io.temporal.activity.ActivityInterface
import io.temporal.activity.ActivityMethod

@ActivityInterface
interface WorkoutAgentActivities {
	@ActivityMethod
	fun runAgent(input: AgentTurnInput): String
}
