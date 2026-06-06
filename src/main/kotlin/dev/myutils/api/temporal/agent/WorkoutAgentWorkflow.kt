package dev.myutils.api.temporal.agent

import io.temporal.workflow.WorkflowInterface
import io.temporal.workflow.WorkflowMethod

@WorkflowInterface
interface WorkoutAgentWorkflow {
	@WorkflowMethod
	fun handleTurn(input: AgentTurnInput)
}
