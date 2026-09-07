package workout

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"reflect"
	"testing"
)

func TestBuildGridKeepsEmptyExercisesAndOrdersDatesAcrossExercises(t *testing.T) {
	exercises := []Exercise{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c", Name: "Empty"}}
	grid := buildGrid(exercises, []entry{
		{ExerciseID: "a", Date: "2030-01-01", Weight: 20, SetCount: 2, RepsPerSet: 10, MaxReps: 10, SetReps: "10,8"},
		{ExerciseID: "b", Date: "2030-01-03", Weight: 30, SetCount: 1, RepsPerSet: 10, MaxReps: 10},
		{ExerciseID: "b", Date: "2030-01-01", Weight: 30, SetCount: 1, RepsPerSet: 10, MaxReps: 10},
	})
	if !reflect.DeepEqual(grid.Dates, []string{"2030-01-03", "2030-01-01"}) {
		t.Fatalf("dates: %v", grid.Dates)
	}
	if len(grid.Rows) != 3 || grid.Rows[2].Cells == nil || len(grid.Rows[2].Cells) != 0 {
		t.Fatalf("empty row: %+v", grid.Rows)
	}
	if got := grid.Rows[0].Cells["2030-01-01"]; !reflect.DeepEqual(got.SetReps, []int{10, 8}) || got.Display == "" {
		t.Fatalf("variable reps lost: %+v", got)
	}
	data, err := json.Marshal(Snapshot{Exercises: []Exercise{}, Grid: buildGrid(nil, nil)})
	if err != nil || string(data) != `{"exercises":[],"grid":{"dates":[],"rows":[]}}` {
		t.Fatalf("empty response: %s %v", data, err)
	}
}

func TestSnapshotUsesOwnedExercisesAndPreservesStoredCells(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := NewService(pool)
	group := "back"
	exercise, err := service.CreateExercise(ctx, CreateExerciseRequest{Name: "Snapshot QA variable sets", MuscleGroup: &group})
	if err != nil {
		t.Fatal(err)
	}
	defer service.DeleteExercise(ctx, exercise.ID)
	empty, err := service.CreateExercise(ctx, CreateExerciseRequest{Name: "Snapshot QA empty"})
	if err != nil {
		t.Fatal(err)
	}
	defer service.DeleteExercise(ctx, empty.ID)
	err = service.UpsertEntry(ctx, EntryRequest{ExerciseID: exercise.ID, PerformedOn: "2039-04-07", WeightKg: 22.5, SetCount: 2, RepsPerSet: 10, MaxReps: 10, SetReps: []int{10, 8}})
	if err != nil {
		t.Fatal(err)
	}
	var foreignID string
	err = pool.QueryRow(ctx, `INSERT INTO exercises(user_id,name,muscle_group) SELECT id,'Snapshot QA foreign','legs' FROM users WHERE email='dev@example.com' RETURNING id::text`).Scan(&foreignID)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM exercises WHERE id=$1::uuid`, foreignID)
	snapshot, err := service.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found, foundEmpty := false, false
	for i, ex := range snapshot.Exercises {
		if ex.ID == foreignID {
			t.Fatal("foreign exercise leaked")
		}
		row := snapshot.Grid.Rows[i]
		if row.ExerciseID != ex.ID || row.ExerciseName != ex.Name {
			t.Fatal("picker and grid disagree")
		}
		if ex.ID == exercise.ID {
			found = true
			cell := row.Cells["2039-04-07"]
			if ex.MuscleGroup != "back" || cell.WeightKg != 22.5 || !reflect.DeepEqual(cell.SetReps, []int{10, 8}) {
				t.Fatalf("stored data changed: %+v %+v", ex, cell)
			}
		}
		if ex.ID == empty.ID {
			foundEmpty = true
			if len(row.Cells) != 0 {
				t.Fatal("empty exercise has cells")
			}
		}
	}
	if !found || !foundEmpty {
		t.Fatal("missing exercises")
	}
	grid, err := service.Grid(ctx)
	if err != nil || !reflect.DeepEqual(grid, snapshot.Grid) {
		t.Fatal("legacy grid diverged", err)
	}
}
