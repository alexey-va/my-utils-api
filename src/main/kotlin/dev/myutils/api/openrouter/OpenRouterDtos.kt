package dev.myutils.api.openrouter

import com.fasterxml.jackson.annotation.JsonIgnoreProperties
import com.fasterxml.jackson.annotation.JsonProperty
import com.fasterxml.jackson.databind.JsonNode

@JsonIgnoreProperties(ignoreUnknown = true)
data class ChatCompletionRequest(
	val model: String,
	val messages: List<ChatMessage>,
	val tools: List<ToolDefinition>? = null,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class ChatMessage(
	val role: String,
	val content: String? = null,
	@JsonProperty("tool_calls")
	val toolCalls: List<ToolCall>? = null,
	@JsonProperty("tool_call_id")
	val toolCallId: String? = null,
	val name: String? = null,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class ToolDefinition(
	val type: String = "function",
	val function: ToolFunction,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class ToolFunction(
	val name: String,
	val description: String,
	val parameters: JsonNode,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class ToolCall(
	val id: String,
	val type: String = "function",
	val function: ToolCallFunction,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class ToolCallFunction(
	val name: String,
	val arguments: String,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class ChatCompletionResponse(
	val choices: List<ChatChoice> = emptyList(),
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class ChatChoice(
	val message: ChatMessage,
)
