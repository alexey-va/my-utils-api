package dev.myutils.api.properties

import com.fasterxml.jackson.databind.JsonNode
import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.agent.AgentSystemPromptDefault
import dev.myutils.api.config.MyUtilsProperties
import dev.myutils.api.temporal.TemporalWorkflowService
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.ObjectProvider
import org.springframework.http.HttpStatus
import org.springframework.web.server.ResponseStatusException
import java.time.ZoneId
import kotlin.reflect.full.memberProperties

enum class PropertyEditor {
	DEFAULT,
	TEXTAREA,
}

enum class PropertyType {
	BOOLEAN,
	INT,
	LONG,
	DOUBLE,
	STRING,
	/** Значение — конкретный data class, см. [Property.objectType]. */
	OBJECT,
}

/** Runtime property — объявите val в [AppProperties], реестр через reflection. */
sealed interface Property<T> {
	val key: String
	val type: PropertyType
	val objectType: String?
	val description: String
	val default: T
	val editor: PropertyEditor get() = PropertyEditor.DEFAULT
	val onApplied: ((PropertyApplyContext) -> Unit)?

	fun serialize(
		value: T,
		mapper: ObjectMapper,
	): String

	fun deserialize(
		raw: String,
		mapper: ObjectMapper,
	): T

	fun normalize(
		node: JsonNode,
		mapper: ObjectMapper,
	): String {
		val value = deserializeFromJson(node, mapper)
		return serialize(value, mapper)
	}

	fun deserializeFromJson(
		node: JsonNode,
		mapper: ObjectMapper,
	): T

	fun storedDefault(mapper: ObjectMapper): String = serialize(default, mapper)

	fun get(): T = RuntimePropertiesAccess.read(this)
}

internal object RuntimePropertiesAccess {
	@Volatile
	private var service: RuntimePropertiesService? = null

	fun bind(service: RuntimePropertiesService) {
		this.service = service
	}

	fun <T> read(property: Property<T>): T =
		requireNotNull(service) {
			"RuntimePropertiesService ещё не инициализирован (вызов ${property.key}.get() до старта приложения?)"
		}.readValue(property)
}

data class PropertyApplyContext(
	val service: RuntimePropertiesService,
	val myUtils: MyUtilsProperties,
	val temporalWorkflow: ObjectProvider<TemporalWorkflowService>,
)

data class PropertyView(
	val key: String,
	val type: PropertyType,
	val objectType: String?,
	val description: String,
	val value: JsonNode,
	val defaultValue: JsonNode,
	val editor: PropertyEditor,
	val updatedAt: java.time.Instant?,
	val updatedBy: String?,
)

object AppProperties {
	val TEMPORAL_EVENING_REMINDER_ENABLED: BooleanProperty =
		BooleanProperty(
			key = "temporal.evening-reminder.enabled",
			description = "Вечернее напоминание в Telegram, если дневник пуст (Temporal workflow).",
			default = false,
			onApplied = PropertySideEffects::refreshEveningReminder,
		)

	val TEMPORAL_EVENING_REMINDER_HOUR: IntProperty =
		IntProperty(
			key = "temporal.evening-reminder.hour",
			description = "Час напоминания (0–23), часовой пояс temporal.zone-id.",
			default = 20,
			range = 0..23,
			onApplied = PropertySideEffects::refreshEveningReminder,
		)

	val TEMPORAL_EVENING_REMINDER_MINUTE: IntProperty =
		IntProperty(
			key = "temporal.evening-reminder.minute",
			description = "Минута напоминания (0–59).",
			default = 0,
			range = 0..59,
			onApplied = PropertySideEffects::refreshEveningReminder,
		)

	val TEMPORAL_ZONE_ID: StringProperty =
		StringProperty(
			key = "temporal.zone-id",
			description = "Часовой пояс для напоминаний и снимка дневника (например Europe/Moscow).",
			default = "Europe/Moscow",
			validate = { runCatching { ZoneId.of(it) }.isSuccess },
			onApplied = PropertySideEffects::refreshEveningReminder,
		)

	val OPENROUTER_MODEL: StringProperty =
		StringProperty(
			key = "openrouter.model",
			description = "Модель OpenRouter для Telegram-агента (формат provider/model-id).",
			default = "anthropic/claude-3.5-haiku",
			validate = { model -> model.length in 3..200 && model.contains('/') },
		)

	val OPENROUTER_MAX_TOOL_ITERATIONS: IntProperty =
		IntProperty(
			key = "openrouter.max-tool-iterations",
			description = "Максимум итераций tool-calling за одно сообщение.",
			default = 8,
			range = 1..32,
		)

	val TELEGRAM_CONVERSATION_TTL_HOURS: IntProperty =
		IntProperty(
			key = "telegram.conversation-ttl-hours",
			description = "Сколько часов хранить историю чата в Redis.",
			default = 48,
			range = 1..(24 * 30),
		)

	val AGENT_SYSTEM_PROMPT: StringProperty =
		StringProperty(
			key = "agent.system-prompt",
			description = "System prompt Telegram-агента (OpenRouter). Редактируется без перезапуска.",
			default = AgentSystemPromptDefault.PROMPT,
			editor = PropertyEditor.TEXTAREA,
			validate = { prompt -> prompt.isNotBlank() && prompt.length <= 32_000 },
		)

	val ALL: List<Property<*>> by lazy { discoverAll() }

	fun find(key: String): Property<*>? = ALL.find { it.key == key }

	private fun discoverAll(): List<Property<*>> {
		val properties =
			AppProperties::class.memberProperties
				.filter { member -> member.name != "ALL" }
				.mapNotNull { member -> member.getter.call(AppProperties) as? Property<*> }
				.sortedBy { it.key }
		val duplicateKeys = properties.groupBy { it.key }.filter { it.value.size > 1 }.keys
		require(duplicateKeys.isEmpty()) {
			"Duplicate runtime property keys: ${duplicateKeys.joinToString()}"
		}
		return properties
	}
}

private object PropertySideEffects {
	private val log = LoggerFactory.getLogger(javaClass)

	fun refreshEveningReminder(ctx: PropertyApplyContext) {
		val temporal = ctx.temporalWorkflow.getIfAvailable() ?: return
		val allowed = ctx.myUtils.telegram.allowedUserIdSet()
		if (allowed.isEmpty()) {
			return
		}
		if (AppProperties.TEMPORAL_EVENING_REMINDER_ENABLED.get()) {
			for (chatId in allowed) {
				temporal.ensureEveningReminderRunning(chatId)
			}
			log.info("Evening reminder workflows ensured after property change")
		} else {
			for (chatId in allowed) {
				temporal.cancelEveningReminder(chatId)
			}
			log.info("Evening reminder workflows cancelled after property change")
		}
	}
}

class BooleanProperty(
	override val key: String,
	override val description: String,
	override val default: Boolean,
	override val onApplied: ((PropertyApplyContext) -> Unit)? = null,
) : Property<Boolean> {
	override val type = PropertyType.BOOLEAN
	override val objectType: String? = null

	override fun serialize(
		value: Boolean,
		mapper: ObjectMapper,
	): String = if (value) "true" else "false"

	override fun deserialize(
		raw: String,
		mapper: ObjectMapper,
	): Boolean =
		when (raw.trim().lowercase()) {
			"true" -> true
			"false" -> false
			else -> throw badRequest("Expected boolean, got: $raw")
		}

	override fun deserializeFromJson(
		node: JsonNode,
		mapper: ObjectMapper,
	): Boolean =
		when {
			node.isBoolean -> node.booleanValue()
			else -> deserialize(node.asText(), mapper)
		}
}

class IntProperty(
	override val key: String,
	override val description: String,
	override val default: Int,
	private val range: IntRange,
	override val onApplied: ((PropertyApplyContext) -> Unit)? = null,
) : Property<Int> {
	override val type = PropertyType.INT
	override val objectType: String? = null

	override fun serialize(
		value: Int,
		mapper: ObjectMapper,
	): String = mapper.writeValueAsString(value)

	override fun deserialize(
		raw: String,
		mapper: ObjectMapper,
	): Int {
		val v =
			runCatching { mapper.readValue(raw.trim(), Int::class.java) }.getOrElse {
				throw badRequest("Expected int, got: $raw")
			}
		if (v !in range) {
			throw badRequest("$key must be in $range")
		}
		return v
	}

	override fun deserializeFromJson(
		node: JsonNode,
		mapper: ObjectMapper,
	): Int = deserialize(mapper.writeValueAsString(node), mapper)
}

class StringProperty(
	override val key: String,
	override val description: String,
	override val default: String,
	private val validate: (String) -> Boolean = { true },
	override val editor: PropertyEditor = PropertyEditor.DEFAULT,
	override val onApplied: ((PropertyApplyContext) -> Unit)? = null,
) : Property<String> {
	override val type = PropertyType.STRING
	override val objectType: String? = null

	override fun serialize(
		value: String,
		mapper: ObjectMapper,
	): String = mapper.writeValueAsString(value)

	override fun deserialize(
		raw: String,
		mapper: ObjectMapper,
	): String {
		val value = mapper.readValue(raw.trim(), String::class.java)
		if (!validate(value)) {
			throw badRequest("Invalid value for $key: $value")
		}
		return value
	}

	override fun deserializeFromJson(
		node: JsonNode,
		mapper: ObjectMapper,
	): String =
		deserialize(
			if (node.isTextual) mapper.writeValueAsString(node.asText()) else mapper.writeValueAsString(node),
			mapper,
		)
}

class DataProperty<T : Any>(
	override val key: String,
	override val description: String,
	override val default: T,
	private val valueClass: Class<T>,
	override val onApplied: ((PropertyApplyContext) -> Unit)? = null,
	private val validate: (T) -> Boolean = { true },
) : Property<T> {
	override val type = PropertyType.OBJECT
	override val objectType: String = valueClass.simpleName

	override fun serialize(
		value: T,
		mapper: ObjectMapper,
	): String = mapper.writeValueAsString(value)

	override fun deserialize(
		raw: String,
		mapper: ObjectMapper,
	): T {
		val value = mapper.readValue(raw, valueClass)
		if (!validate(value)) {
			throw badRequest("Invalid value for $key ($objectType)")
		}
		return value
	}

	override fun deserializeFromJson(
		node: JsonNode,
		mapper: ObjectMapper,
	): T = deserialize(mapper.writeValueAsString(node), mapper)
}

inline fun <reified T : Any> dataProperty(
	key: String,
	description: String,
	default: T,
	noinline onApplied: ((PropertyApplyContext) -> Unit)? = null,
	noinline validate: (T) -> Boolean = { true },
): DataProperty<T> =
	DataProperty(
		key = key,
		description = description,
		default = default,
		valueClass = T::class.java,
		onApplied = onApplied,
		validate = validate,
	)

internal fun badRequest(message: String): ResponseStatusException =
	ResponseStatusException(HttpStatus.BAD_REQUEST, message)
