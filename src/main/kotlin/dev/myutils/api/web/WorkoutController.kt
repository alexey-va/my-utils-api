package dev.myutils.api.web

import dev.myutils.api.service.WorkoutService
import dev.myutils.api.web.dto.CreateExerciseRequest
import dev.myutils.api.web.dto.UpdateExerciseRequest
import dev.myutils.api.web.dto.ExerciseProgressResponse
import dev.myutils.api.web.dto.ExerciseResponse
import dev.myutils.api.web.dto.UpsertWorkoutEntryRequest
import dev.myutils.api.web.dto.WorkoutGridResponse
import jakarta.validation.Valid
import org.springframework.http.HttpStatus
import org.springframework.web.bind.annotation.DeleteMapping
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PatchMapping
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.ResponseStatus
import org.springframework.web.bind.annotation.RestController
import java.util.UUID

@RestController
@RequestMapping("/api/workouts")
class WorkoutController(
	private val workoutService: WorkoutService,
) {
	@GetMapping("/exercises")
	fun listExercises(): List<ExerciseResponse> = workoutService.listExercises()

	@PostMapping("/exercises")
	fun createExercise(@Valid @RequestBody body: CreateExerciseRequest): ExerciseResponse =
		workoutService.createExercise(body)

	@PatchMapping("/exercises/{id}")
	fun updateExercise(
		@PathVariable id: UUID,
		@Valid @RequestBody body: UpdateExerciseRequest,
	): ExerciseResponse = workoutService.updateExercise(id, body)

	@GetMapping("/exercises/{id}/progress")
	fun progress(@PathVariable id: UUID): ExerciseProgressResponse = workoutService.getProgress(id)

	@DeleteMapping("/exercises/{id}")
	@ResponseStatus(HttpStatus.NO_CONTENT)
	fun deleteExercise(@PathVariable id: UUID) {
		workoutService.deleteExercise(id)
	}

	@GetMapping("/grid")
	fun grid(): WorkoutGridResponse = workoutService.getGrid()

	@PostMapping("/entries")
	@ResponseStatus(HttpStatus.NO_CONTENT)
	fun upsertEntry(@Valid @RequestBody body: UpsertWorkoutEntryRequest) {
		workoutService.upsertEntry(body)
	}
}
