package httpapi

import (
	"context"
	"errors"
	"github.com/alexey-va/my-utils-api/internal/workout"
	"net/http"
	"net/http/httptest"
	"testing"
)

type snapshotWorkout struct {
	WorkoutService
	err error
}

func (s snapshotWorkout) Snapshot(context.Context) (workout.Snapshot, error) {
	return workout.Snapshot{Exercises: []workout.Exercise{}, Grid: workout.Grid{Dates: []string{}, Rows: []workout.GridRow{}}}, s.err
}
func TestWorkoutSnapshotPublicReadAndServerError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"public", nil, http.StatusOK}, {"storage failure", errors.New("database password must not leak"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter(Dependencies{Auth: fakeAuth{}, Workout: snapshotWorkout{err: tc.err}})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/workouts/snapshot", nil))
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.err != nil && response.Body.String() != "{\"message\":\"Internal server error\"}\n" {
				t.Fatalf("error body=%q", response.Body.String())
			}
		})
	}
}
