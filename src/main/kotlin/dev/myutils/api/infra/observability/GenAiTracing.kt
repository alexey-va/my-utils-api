package dev.myutils.api.infra.observability

import dev.myutils.api.infra.util.LogPreview
import dev.myutils.api.temporal.agent.AgentLlmStepResult
import io.opentelemetry.api.GlobalOpenTelemetry
import io.opentelemetry.api.trace.Span
import io.opentelemetry.api.trace.SpanKind
import io.opentelemetry.api.trace.StatusCode
import io.opentelemetry.api.trace.Tracer
import io.opentelemetry.context.Context
import io.opentelemetry.context.propagation.TextMapGetter
import io.opentelemetry.context.propagation.TextMapSetter
import java.util.concurrent.ConcurrentHashMap

/** OpenTelemetry gen_ai spans for the workout Telegram agent (local Tempo / Grafana). */
object GenAiTracing {
	private const val INSTRUMENTATION = "dev.myutils.api.agent"
	private const val AGENT_NAME = "workout-telegram-agent"
	private const val PROVIDER = "openrouter"

	private val tracer: Tracer = GlobalOpenTelemetry.getTracer(INSTRUMENTATION)
	private val propagator = GlobalOpenTelemetry.getPropagators().textMapPropagator

	private val traceParentGetter =
		object : TextMapGetter<Map<String, String>> {
			override fun keys(carrier: Map<String, String>): Iterable<String> = carrier.keys

			override fun get(
				carrier: Map<String, String>?,
				key: String,
			): String? = carrier?.get(key)
		}

	private val traceParentSetter =
		object : TextMapSetter<MutableMap<String, String>> {
			override fun set(
				carrier: MutableMap<String, String>?,
				key: String,
				value: String,
			) {
				carrier?.put(key, value)
			}
		}

	fun currentTraceParent(): String? {
		val carrier = mutableMapOf<String, String>()
		propagator.inject(Context.current(), carrier, traceParentSetter)
		return carrier["traceparent"]
	}

	fun <T> invokeAgent(
		chatId: Long,
		userId: Long,
		userText: String,
		block: () -> T,
	): T =
		span(
			name = "invoke_agent $AGENT_NAME",
			kind = SpanKind.INTERNAL,
			operation = "invoke_agent",
			chatId = chatId,
			parentTraceParent = null,
		) { span ->
			span.setAttribute("gen_ai.agent.name", AGENT_NAME)
			span.setAttribute("telegram.user.id", userId)
			addUserMessageEvent(span, userText)
			block()
		}

	fun chat(
		traceParent: String?,
		chatId: Long,
		model: String,
		userMessage: String?,
		block: () -> AgentLlmStepResult,
	): AgentLlmStepResult =
		span(
			name = "chat $model",
			kind = SpanKind.CLIENT,
			operation = "chat",
			chatId = chatId,
			parentTraceParent = traceParent,
		) { span ->
			span.setAttribute("gen_ai.request.model", model)
			span.setAttribute("gen_ai.provider.name", PROVIDER)
			if (!userMessage.isNullOrBlank()) {
				addUserMessageEvent(span, userMessage)
			}
			runCatching { block() }
				.onSuccess { result -> recordChatResult(span, result) }
				.onFailure { error -> span.recordException(error) }
				.getOrThrow()
		}

	fun <T> executeTool(
		traceParent: String?,
		chatId: Long,
		toolName: String,
		toolCallId: String?,
		argumentsJson: String,
		block: () -> T,
	): T =
		span(
			name = "execute_tool $toolName",
			kind = SpanKind.INTERNAL,
			operation = "execute_tool",
			chatId = chatId,
			parentTraceParent = traceParent,
		) { span ->
			span.setAttribute("gen_ai.tool.name", toolName)
			toolCallId?.let { span.setAttribute("gen_ai.tool.call.id", it) }
			val argsPreview = LogPreview.of(argumentsJson, max = 2_000)
			if (argsPreview.isNotBlank()) {
				span.setAttribute("gen_ai.tool.call.arguments", argsPreview)
			}
			runCatching { block() }
				.onSuccess { result ->
					val preview = LogPreview.of(result?.toString().orEmpty(), max = 2_000)
					if (preview.isNotBlank()) {
						span.setAttribute("gen_ai.tool.call.result", preview)
					}
				}.onFailure { error -> span.recordException(error) }
				.getOrThrow()
		}

	private fun recordChatResult(
		span: Span,
		result: AgentLlmStepResult,
	) {
		if (result.reply.isNotBlank()) {
			span.addEvent(
				"gen_ai.assistant.message",
				io.opentelemetry.api.common.Attributes.of(
					io.opentelemetry.api.common.AttributeKey.stringKey("message"),
					LogPreview.of(result.reply, max = 2_000),
				),
			)
		}
		if (result.toolCalls.isNotEmpty()) {
			span.setAttribute("gen_ai.tool.call.count", result.toolCalls.size.toLong())
			result.toolCalls.forEach { call ->
				span.addEvent(
					"gen_ai.tool.call",
					io.opentelemetry.api.common.Attributes.of(
						io.opentelemetry.api.common.AttributeKey.stringKey("gen_ai.tool.name"),
						call.name,
						io.opentelemetry.api.common.AttributeKey.stringKey("gen_ai.tool.call.id"),
						call.id,
					),
				)
			}
		}
	}

	private fun addUserMessageEvent(
		span: Span,
		userText: String,
	) {
		val preview = LogPreview.of(userText, max = 2_000)
		if (preview.isBlank()) {
			return
		}
		span.addEvent(
			"gen_ai.user.message",
			io.opentelemetry.api.common.Attributes.of(
				io.opentelemetry.api.common.AttributeKey.stringKey("message"),
				preview,
			),
		)
	}

	private fun <T> span(
		name: String,
		kind: SpanKind,
		operation: String,
		chatId: Long,
		parentTraceParent: String?,
		block: (Span) -> T,
	): T {
		val parentContext = extractParent(parentTraceParent)
		val span =
			tracer
				.spanBuilder(name)
				.setSpanKind(kind)
				.setParent(parentContext)
				.setAttribute("gen_ai.operation.name", operation)
				.setAttribute("gen_ai.conversation.id", chatId.toString())
				.startSpan()
		return span.makeCurrent().use {
			try {
				block(span)
			} catch (error: Exception) {
				span.setStatus(StatusCode.ERROR)
				span.recordException(error)
				throw error
			} finally {
				span.end()
			}
		}
	}

	private fun extractParent(traceParent: String?): Context {
		if (traceParent.isNullOrBlank()) {
			return Context.current()
		}
		val carrier = ConcurrentHashMap<String, String>()
		carrier["traceparent"] = traceParent
		return propagator.extract(Context.root(), carrier, traceParentGetter)
	}
}
