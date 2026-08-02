package dev.myutils.api.web

import dev.myutils.api.agent.memory.AgentMemoryChatTurnResult
import dev.myutils.api.agent.memory.AgentMemoryMessagePage
import dev.myutils.api.agent.memory.AgentTestChatDto
import dev.myutils.api.agent.memory.AgentTestChatService
import dev.myutils.api.web.dto.AgentChatTurnRequest
import dev.myutils.api.web.dto.CreateAgentTestChatRequest
import dev.myutils.api.web.dto.RenameAgentTestChatRequest
import jakarta.validation.Valid
import org.springframework.http.HttpStatus
import org.springframework.web.bind.annotation.DeleteMapping
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PatchMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.ResponseStatus
import org.springframework.web.bind.annotation.RestController
import java.util.UUID

@RestController
@RequestMapping("/api/admin/agent-test-chats")
class AdminAgentTestChatController(
	private val service: AgentTestChatService,
) {
	@PostMapping
	@ResponseStatus(HttpStatus.CREATED)
	fun create(
		@Valid @RequestBody body: CreateAgentTestChatRequest,
	): AgentTestChatDto = service.create(body.title)

	@GetMapping
	fun list(): List<AgentTestChatDto> = service.list()

	@GetMapping("/{id}")
	fun get(
		@PathVariable id: UUID,
	): AgentTestChatDto = service.get(id)

	@PatchMapping("/{id}")
	fun rename(
		@PathVariable id: UUID,
		@Valid @RequestBody body: RenameAgentTestChatRequest,
	): AgentTestChatDto = service.rename(id, body.title)

	@DeleteMapping("/{id}")
	@ResponseStatus(HttpStatus.NO_CONTENT)
	fun delete(
		@PathVariable id: UUID,
	) {
		service.delete(id)
	}

	@GetMapping("/{id}/messages")
	fun listMessages(
		@PathVariable id: UUID,
		@RequestParam(required = false) beforeId: Long?,
		@RequestParam(defaultValue = "50") limit: Int,
	): AgentMemoryMessagePage = service.listMessages(id, beforeId, limit)

	@PostMapping("/{id}/messages")
	fun sendMessage(
		@PathVariable id: UUID,
		@Valid @RequestBody body: AgentChatTurnRequest,
	): AgentMemoryChatTurnResult = service.sendMessage(id, body.content, body.images)

	@DeleteMapping("/{id}/messages")
	@ResponseStatus(HttpStatus.NO_CONTENT)
	fun clearMessages(
		@PathVariable id: UUID,
	) {
		service.clearMessages(id)
	}
}
