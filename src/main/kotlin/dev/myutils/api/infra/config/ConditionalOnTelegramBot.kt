package dev.myutils.api.infra.config

import org.springframework.context.annotation.Condition
import org.springframework.context.annotation.ConditionContext
import org.springframework.context.annotation.Conditional
import org.springframework.core.type.AnnotatedTypeMetadata

@Target(AnnotationTarget.CLASS, AnnotationTarget.FUNCTION)
@Retention(AnnotationRetention.RUNTIME)
@Conditional(OnTelegramBotCondition::class)
annotation class ConditionalOnTelegramBot

class OnTelegramBotCondition : Condition {
	override fun matches(
		context: ConditionContext,
		metadata: AnnotatedTypeMetadata,
	): Boolean {
		val enabled =
			context.environment.getProperty("myutils.telegram.enabled", Boolean::class.java, false)
		val token =
			context.environment.getProperty("myutils.telegram.bot-token")
				?: context.environment.getProperty("TELEGRAM_BOT_TOKEN")
		return enabled && !token.isNullOrBlank()
	}
}
