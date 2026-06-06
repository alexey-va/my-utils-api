package dev.myutils.api.properties

import com.fasterxml.jackson.databind.JsonNode
import com.fasterxml.jackson.databind.ObjectMapper
import dev.myutils.api.infra.config.MyUtilsProperties
import dev.myutils.api.domain.AppSetting
import dev.myutils.api.domain.AppSettingRepository
import dev.myutils.api.temporal.TemporalWorkflowService
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.ObjectProvider
import org.springframework.boot.context.event.ApplicationReadyEvent
import org.springframework.context.event.EventListener
import org.springframework.http.HttpStatus
import org.springframework.scheduling.annotation.Scheduled
import org.springframework.stereotype.Service
import org.springframework.transaction.annotation.Transactional
import org.springframework.web.server.ResponseStatusException
import java.time.Instant
import java.util.concurrent.ConcurrentHashMap

@Service
class RuntimePropertiesService(
	private val repository: AppSettingRepository,
	private val objectMapper: ObjectMapper,
	private val myUtils: MyUtilsProperties,
	private val temporalWorkflow: ObjectProvider<TemporalWorkflowService>,
) {
	private val log = LoggerFactory.getLogger(javaClass)
	private val cache = ConcurrentHashMap<String, String>()

	init {
		RuntimePropertiesAccess.bind(this)
	}

	internal fun <T> readValue(property: Property<T>): T = read(property)

	@Transactional(readOnly = true)
	fun listAll(): List<PropertyView> =
		AppProperties.ALL.map { definition ->
			val entity = repository.findById(definition.key).orElse(null)
			toView(definition, entity)
		}

	@Transactional
	fun update(
		key: String,
		value: JsonNode,
		updatedBy: String?,
	): PropertyView {
		val property =
			AppProperties.find(key)
				?: throw ResponseStatusException(HttpStatus.NOT_FOUND, "Unknown property: $key")
		val normalized =
			try {
				property.normalize(value, objectMapper)
			} catch (ex: ResponseStatusException) {
				throw ex
			} catch (ex: Exception) {
				throw ResponseStatusException(HttpStatus.BAD_REQUEST, ex.message ?: "Invalid property value")
			}
		val entity =
			repository.findById(key).orElseGet {
				AppSetting(
					key = key,
					value = property.storedDefault(objectMapper),
					tags = property.tags,
				)
			}
		entity.value = normalized
		entity.updatedAt = Instant.now()
		entity.updatedBy = updatedBy
		repository.save(entity)
		cache[key] = normalized
		log.info("Runtime property updated key={} by={}", key, updatedBy)
		property.onApplied?.invoke(applyContext())
		return toView(property, entity)
	}

	@EventListener(ApplicationReadyEvent::class)
	@Transactional
	fun onApplicationReady() {
		ensureDefaultsSeeded()
	}

	@Scheduled(fixedRateString = "PT1M", initialDelayString = "PT1M")
	@Transactional
	fun refreshFromDatabase() {
		try {
			reloadFromDatabase()
		} catch (ex: Exception) {
			log.error("Не удалось перезагрузить runtime-свойства из БД: {}", ex.message, ex)
		}
	}

	@Transactional
	fun reloadFromDatabase() {
		val fromDb = repository.findAll().associate { it.key to it.value }
		for (property in AppProperties.ALL) {
			val raw = fromDb[property.key] ?: property.storedDefault(objectMapper)
			cache[property.key] = sanitizeRaw(property, raw)
		}
		log.debug("Runtime properties cache reloaded, {} keys", cache.size)
	}

	@Transactional
	fun ensureDefaultsSeeded() {
		for (property in AppProperties.ALL) {
			if (!repository.existsById(property.key)) {
				repository.save(
					AppSetting(
						key = property.key,
						value = property.storedDefault(objectMapper),
						tags = property.tags,
					),
				)
				log.info("Seeded default runtime property {}", property.key)
			}
		}
		syncTagsFromDefinitions()
		reloadFromDatabase()
	}

	private fun syncTagsFromDefinitions() {
		for (property in AppProperties.ALL) {
			val entity = repository.findById(property.key).orElse(null) ?: continue
			if (entity.tags != property.tags) {
				entity.tags = property.tags
				repository.save(entity)
				log.info("Synced tags for runtime property {}", property.key)
			}
		}
	}

	private fun <T> read(property: Property<T>): T {
		val rawValue = raw(property)
		return runCatching {
			property.deserialize(rawValue, objectMapper)
		}.getOrElse { ex ->
			log.error(
				"Ошибка парсинга runtime-свойства {} (raw={}): {}; используем значение по умолчанию",
				property.key,
				rawValue,
				ex.message,
			)
			property.default
		}
	}

	private fun sanitizeRaw(
		property: Property<*>,
		raw: String,
	): String =
		runCatching {
			property.deserialize(raw, objectMapper)
			raw
		}.getOrElse { ex ->
			log.error(
				"Ошибка парсинга runtime-свойства {} при перезагрузке (raw={}): {}; используем значение по умолчанию",
				property.key,
				raw,
				ex.message,
			)
			property.storedDefault(objectMapper)
		}

	private fun raw(property: Property<*>): String =
		cache[property.key]
			?: repository.findById(property.key).map { it.value }.orElse(property.storedDefault(objectMapper))

	private fun toView(
		property: Property<*>,
		entity: AppSetting?,
	): PropertyView {
		val defaultStored = property.storedDefault(objectMapper)
		val valueRaw = entity?.value ?: defaultStored
		val safeRaw =
			runCatching {
				property.deserialize(valueRaw, objectMapper)
				valueRaw
			}.getOrElse { defaultStored }
		return PropertyView(
			key = property.key,
			type = property.type,
			objectType = property.objectType,
			description = property.description,
			tags = entity?.tags?.takeIf { it.isNotEmpty() } ?: property.tags,
			value = objectMapper.readTree(safeRaw),
			defaultValue = objectMapper.readTree(defaultStored),
			editor = property.editor,
			updatedAt = entity?.updatedAt,
			updatedBy = entity?.updatedBy,
		)
	}

	private fun applyContext() =
		PropertyApplyContext(
			service = this,
			myUtils = myUtils,
			temporalWorkflow = temporalWorkflow,
		)
}
