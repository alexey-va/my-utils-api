package dev.myutils.api.telegram

import com.fasterxml.jackson.annotation.JsonIgnoreProperties
import com.fasterxml.jackson.annotation.JsonInclude
import com.fasterxml.jackson.annotation.JsonProperty

@JsonIgnoreProperties(ignoreUnknown = true)
data class TelegramUpdate(
	@JsonProperty("update_id")
	val updateId: Long? = null,
	val message: TelegramMessage? = null,
	@JsonProperty("edited_message")
	val editedMessage: TelegramMessage? = null,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class TelegramMessage(
	@JsonProperty("message_id")
	val messageId: Long? = null,
	val from: TelegramUser? = null,
	val chat: TelegramChat,
	val text: String? = null,
	val date: Long? = null,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class TelegramUser(
	val id: Long,
	val username: String? = null,
	@JsonProperty("first_name")
	val firstName: String? = null,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class TelegramChat(
	val id: Long,
	val type: String? = null,
)

@JsonIgnoreProperties(ignoreUnknown = true)
@JsonInclude(JsonInclude.Include.NON_NULL)
data class SendMessageRequest(
	@JsonProperty("chat_id")
	val chatId: Long,
	val text: String,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class SendChatActionRequest(
	@JsonProperty("chat_id")
	val chatId: Long,
	val action: String,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class SetWebhookRequest(
	val url: String,
	@JsonProperty("allowed_updates")
	val allowedUpdates: List<String> = listOf("message", "edited_message"),
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class TelegramApiResponse<T>(
	val ok: Boolean = false,
	val result: T? = null,
	val description: String? = null,
)

@JsonIgnoreProperties(ignoreUnknown = true)
data class TelegramUpdatesResult(
	val ok: Boolean = false,
	val result: List<TelegramUpdate> = emptyList(),
	val description: String? = null,
)
