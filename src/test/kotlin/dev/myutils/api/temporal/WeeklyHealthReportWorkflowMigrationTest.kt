package dev.myutils.api.temporal

import io.temporal.api.common.v1.WorkflowExecution
import io.temporal.client.WorkflowClient
import io.temporal.client.WorkflowNotFoundException
import io.temporal.client.WorkflowStub
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import org.mockito.kotlin.mock
import org.mockito.kotlin.verify
import org.mockito.kotlin.whenever

class WeeklyHealthReportWorkflowMigrationTest {
	@Test
	fun `uses a versioned id for the Saturday workflow`() {
		assertEquals(
			"weekly-health-report-v2-42",
			TemporalWorkflowService.weeklyHealthReportWorkflowId(42),
		)
		assertEquals(
			"weekly-health-report-42",
			TemporalWorkflowService.legacyWeeklyHealthReportWorkflowId(42),
		)
	}

	@Test
	fun `terminates the legacy Sunday workflow after the replacement starts`() {
		val client: WorkflowClient = mock()
		val legacyStub: WorkflowStub = mock()
		whenever(client.newUntypedWorkflowStub("weekly-health-report-42")).thenReturn(legacyStub)

		terminateLegacyWeeklyHealthReport(client, 42)

		verify(legacyStub).terminate("Migrated weekly health report schedule from Sunday to Saturday")
	}

	@Test
	fun `ignores an absent legacy Sunday workflow`() {
		val client: WorkflowClient = mock()
		whenever(client.newUntypedWorkflowStub("weekly-health-report-42"))
			.thenThrow(
				WorkflowNotFoundException(
					WorkflowExecution.getDefaultInstance(),
					"WeeklyHealthReportWorkflow",
					null,
				),
			)

		terminateLegacyWeeklyHealthReport(client, 42)
	}
}
