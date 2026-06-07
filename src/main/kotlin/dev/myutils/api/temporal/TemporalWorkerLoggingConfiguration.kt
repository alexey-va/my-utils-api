package dev.myutils.api.temporal

import io.temporal.spring.boot.TemporalOptionsCustomizer
import io.temporal.worker.WorkerFactoryOptions
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration

/** Включает replay-safe логи из {@link io.temporal.workflow.Workflow#getLogger}. */
@Configuration
@ConditionalOnProperty(prefix = "myutils.temporal", name = ["enabled"], havingValue = "true")
class TemporalWorkerLoggingConfiguration {
	@Bean
	fun workerFactoryLoggingCustomizer(): TemporalOptionsCustomizer<WorkerFactoryOptions.Builder> =
		TemporalOptionsCustomizer { builder ->
			builder.setEnableLoggingInReplay(true)
		}
}
