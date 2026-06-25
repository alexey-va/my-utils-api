package dev.myutils.api.web

import dev.myutils.api.agent.memory.AgentMemoryAdminService
import dev.myutils.api.agent.memory.AgentMemoryCompactResult
import dev.myutils.api.agent.memory.AgentMemoryChatDetail
import dev.myutils.api.agent.memory.AgentMemoryChatSummary
import dev.myutils.api.agent.memory.AgentMemoryFactDto
import dev.myutils.api.agent.memory.AgentMemoryMessageDto
import dev.myutils.api.agent.memory.AgentMemoryMessagePage
import dev.myutils.api.web.dto.CreateAgentFactRequest
import dev.myutils.api.web.dto.ResetCompactionResponse
import dev.myutils.api.web.dto.UpdateAgentFactRequest
import dev.myutils.api.web.dto.UpdateMessageExcludedRequest
import jakarta.validation.Valid
import org.springframework.web.bind.annotation.DeleteMapping
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PatchMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.PutMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController
import java.util.UUID

@RestController
@RequestMapping("/api/admin/agent-memory")
class AdminAgentMemoryController(
	private val service: AgentMemoryAdminService,
) {
	@GetMapping("/chats")
	fun listChats(): List<AgentMemoryChatSummary> = service.listChats()

	@GetMapping("/chats/{chatId}")
	fun getChat(
		@PathVariable chatId: Long,
	): AgentMemoryChatDetail = service.getChat(chatId)

	@GetMapping("/chats/{chatId}/messages")
	fun listMessages(
		@PathVariable chatId: Long,
		@RequestParam(required = false) beforeId: Long?,
		@RequestParam(defaultValue = "50") limit: Int,
	): AgentMemoryMessagePage = service.listMessages(chatId, beforeId, limit)

	@PostMapping("/chats/{chatId}/facts")
	fun createFact(
		@PathVariable chatId: Long,
		@Valid @RequestBody body: CreateAgentFactRequest,
	): AgentMemoryFactDto = service.createFact(chatId, body.content)

	@PutMapping("/facts/{id}")
	fun updateFact(
		@PathVariable id: UUID,
		@Valid @RequestBody body: UpdateAgentFactRequest,
	): AgentMemoryFactDto = service.updateFact(id, body.content)

	@DeleteMapping("/facts/{id}")
	fun deleteFact(
		@PathVariable id: UUID,
	) {
		service.deleteFact(id)
	}

	@PatchMapping("/messages/{id}")
	fun updateMessageExcluded(
		@PathVariable id: Long,
		@Valid @RequestBody body: UpdateMessageExcludedRequest,
	): AgentMemoryMessageDto = service.updateMessageExcluded(id, body.excludedFromContext)

	@DeleteMapping("/messages/{id}")
	fun deleteMessage(
		@PathVariable id: Long,
	) {
		service.deleteMessage(id)
	}

	@PostMapping("/chats/{chatId}/compact")
	fun compact(
		@PathVariable chatId: Long,
		@RequestParam(defaultValue = "false") force: Boolean,
	): AgentMemoryCompactResult = service.compact(chatId, force)

	@PostMapping("/chats/{chatId}/reset-compaction")
	fun resetCompaction(
		@PathVariable chatId: Long,
	): ResetCompactionResponse = ResetCompactionResponse(service.resetCompaction(chatId))

	@DeleteMapping("/chats/{chatId}/dialog")
	fun clearDialog(
		@PathVariable chatId: Long,
	) {
		service.clearDialog(chatId)
	}
}
