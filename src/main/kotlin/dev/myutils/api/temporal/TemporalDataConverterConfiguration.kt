package dev.myutils.api.temporal

import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper
import io.temporal.common.converter.DataConverter
import io.temporal.common.converter.DefaultDataConverter
import io.temporal.common.converter.JacksonJsonPayloadConverter
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration

/** Temporal default Jackson mapper does not understand Kotlin data classes. */
@Configuration
@ConditionalOnProperty(prefix = "myutils.temporal", name = ["enabled"], havingValue = "true")
class TemporalDataConverterConfiguration {
	@Bean
	fun temporalDataConverter(): DataConverter {
		val mapper = jacksonObjectMapper()
		return DefaultDataConverter.newDefaultInstance().withPayloadConverterOverrides(
			JacksonJsonPayloadConverter(mapper),
		)
	}
}
