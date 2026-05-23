package dev.myutils.api.web.dto

import com.fasterxml.jackson.databind.JsonNode
import dev.myutils.api.properties.PropertyEditor
import dev.myutils.api.properties.PropertyType
import dev.myutils.api.properties.PropertyView
import java.time.Instant

data class PropertyResponse(
	val key: String,
	val type: PropertyType,
	/** Имя data class для type=OBJECT, иначе null. */
	val objectType: String?,
	val description: String,
	val tags: List<String>,
	val value: JsonNode,
	val defaultValue: JsonNode,
	val editor: PropertyEditor,
	val updatedAt: Instant?,
	val updatedBy: String?,
)

data class UpdatePropertyRequest(
	val value: JsonNode,
)

fun PropertyView.toResponse(): PropertyResponse =
	PropertyResponse(
		key = key,
		type = type,
		objectType = objectType,
		description = description,
		tags = tags,
		value = value,
		defaultValue = defaultValue,
		editor = editor,
		updatedAt = updatedAt,
		updatedBy = updatedBy,
	)
