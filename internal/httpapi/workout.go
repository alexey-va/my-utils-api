package httpapi

import (
	"errors"
	"net/http"

	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/go-chi/chi/v5"
)

func (a *API) registerWorkoutRoutes(router chi.Router) {
	if a.workout == nil {
		return
	}
	router.Route("/api/workouts", func(routes chi.Router) {
		routes.Get("/exercises", a.listExercises)
		routes.Post("/exercises", a.createExercise)
		routes.Patch("/exercises/{id}", a.updateExercise)
		routes.Delete("/exercises/{id}", a.deleteExercise)
		routes.Get("/exercises/{id}/progress", a.exerciseProgress)
		routes.Get("/grid", a.workoutGrid)
		routes.Post("/entries", a.upsertWorkoutEntry)
		routes.Post("/entries/move", a.moveWorkoutEntry)
		routes.Delete("/exercises/{exerciseId}/entries/{performedOn}", a.deleteWorkoutEntry)
	})
}

func (a *API) listExercises(response http.ResponseWriter, request *http.Request) {
	result, err := a.workout.ListExercises(request.Context())
	writeDomainResult(response, result, err)
}

func (a *API) createExercise(response http.ResponseWriter, request *http.Request) {
	var body workout.CreateExerciseRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	result, err := a.workout.CreateExercise(request.Context(), body)
	writeDomainResult(response, result, err)
}

func (a *API) updateExercise(response http.ResponseWriter, request *http.Request) {
	var body workout.CreateExerciseRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	result, err := a.workout.UpdateExercise(request.Context(), chi.URLParam(request, "id"), body)
	writeDomainResult(response, result, err)
}

func (a *API) deleteExercise(response http.ResponseWriter, request *http.Request) {
	if err := a.workout.DeleteExercise(request.Context(), chi.URLParam(request, "id")); err != nil {
		writeDomainError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) exerciseProgress(response http.ResponseWriter, request *http.Request) {
	result, err := a.workout.Progress(request.Context(), chi.URLParam(request, "id"))
	writeDomainResult(response, result, err)
}

func (a *API) workoutGrid(response http.ResponseWriter, request *http.Request) {
	result, err := a.workout.Grid(request.Context())
	writeDomainResult(response, result, err)
}

func (a *API) upsertWorkoutEntry(response http.ResponseWriter, request *http.Request) {
	var body workout.EntryRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if err := a.workout.UpsertEntry(request.Context(), body); err != nil {
		writeDomainError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) moveWorkoutEntry(response http.ResponseWriter, request *http.Request) {
	var body workout.MoveRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if err := a.workout.MoveEntry(request.Context(), body); err != nil {
		writeDomainError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteWorkoutEntry(response http.ResponseWriter, request *http.Request) {
	if err := a.workout.DeleteEntry(request.Context(), chi.URLParam(request, "exerciseId"), chi.URLParam(request, "performedOn")); err != nil {
		writeDomainError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func writeDomainResult(response http.ResponseWriter, result any, err error) {
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func writeDomainError(response http.ResponseWriter, err error) {
	var domainError *workout.Error
	if errors.As(err, &domainError) {
		writeError(response, domainError.Status, domainError.Message)
		return
	}
	writeError(response, http.StatusInternalServerError, "Internal server error")
}
