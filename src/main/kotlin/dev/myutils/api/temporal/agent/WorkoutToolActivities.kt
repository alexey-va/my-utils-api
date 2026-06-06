package dev.myutils.api.temporal.agent

import io.temporal.activity.ActivityInterface
import io.temporal.activity.ActivityMethod

@ActivityInterface
interface WorkoutToolActivities {
	@ActivityMethod
	fun executeTool(input: ToolCallInput): String
}
