package dev.myutils.api.agent

import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class AgentToolMutationPolicyTest {
	@Test
	fun `question about a past set cannot authorize workout logging`() {
		assertNotNull(
			AgentToolMutationPolicy.denialReason(
				"logWorkout",
				"Что я делал вчера — плечи 22 кг 10/9?",
			),
		)
	}

	@Test
	fun `statement with workout notation authorizes logging`() {
		assertNull(
			AgentToolMutationPolicy.denialReason(
				"logWorkout",
				"Дак вчера делал плечи 22 кг 10/9",
			),
		)
	}

	@Test
	fun `question containing a weight cannot authorize body weight logging`() {
		assertNotNull(
			AgentToolMutationPolicy.denialReason(
				"logBodyWeight",
				"Какой мой вес — 82.1 кг?",
			),
		)
	}

	@Test
	fun `bare body weight measurement authorizes logging`() {
		assertNull(AgentToolMutationPolicy.denialReason("logBodyWeight", "82.1 кг"))
	}

	@Test
	fun `natural dated body weight measurement authorizes logging`() {
		assertNull(AgentToolMutationPolicy.denialReason("logBodyWeight", "вес сегодня 82.4"))
		assertNull(AgentToolMutationPolicy.denialReason("logBodyWeight", "сегодня вес 82,4 кг"))
	}

	@Test
	fun `read only tools never require mutation authorization`() {
		assertNull(AgentToolMutationPolicy.denialReason("getDaySummaries", null))
	}

	@Test
	fun `natural add exercise command authorizes creation`() {
		assertNull(
			AgentToolMutationPolicy.denialReason(
				"createExercise",
				"добавь жим лежа, грудь",
			),
		)
	}

	@Test
	fun `question about adding exercise stays read only`() {
		assertNotNull(
			AgentToolMutationPolicy.denialReason(
				"createExercise",
				"как добавить жим лежа?",
			),
		)
	}

	@Test
	fun `remember command with colon authorizes fact mutation`() {
		assertNull(
			AgentToolMutationPolicy.denialReason(
				"rememberFact",
				"запомни: локоть беречь",
			),
		)
	}
}
