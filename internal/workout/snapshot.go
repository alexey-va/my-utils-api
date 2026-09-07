package workout

import "context"

// Snapshot keeps the exercise picker and journal on the same database snapshot.
type Snapshot struct {
	Exercises []Exercise `json:"exercises"`
	Grid      Grid       `json:"grid"`
}

func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	userID, err := s.localUserID(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	// One statement sees one PostgreSQL MVCC snapshot, including empty exercises.
	rows, err := s.pool.Query(ctx, `
		SELECT e.id::text, e.name, e.muscle_group,
			COALESCE(w.performed_on::text,''), COALESCE(w.weight_kg,0),
			COALESCE(w.set_count,0), COALESCE(w.reps_per_set,0), COALESCE(w.max_reps,0),
			COALESCE(w.set_reps,''), COALESCE(w.set_weights,'')
		FROM exercises e
		LEFT JOIN workout_entries w ON w.exercise_id=e.id AND w.user_id=e.user_id
		WHERE e.user_id=$1::uuid
		ORDER BY e.name ASC, e.id, w.performed_on DESC`, userID)
	if err != nil {
		return Snapshot{}, err
	}
	defer rows.Close()
	exercises := make([]Exercise, 0)
	entries := make([]entry, 0)
	for rows.Next() {
		var exercise Exercise
		var item entry
		if err := rows.Scan(&exercise.ID, &exercise.Name, &exercise.MuscleGroup,
			&item.Date, &item.Weight, &item.SetCount, &item.RepsPerSet, &item.MaxReps, &item.SetReps, &item.SetWeights); err != nil {
			return Snapshot{}, err
		}
		if len(exercises) == 0 || exercises[len(exercises)-1].ID != exercise.ID {
			exercises = append(exercises, exercise)
		}
		if item.Date != "" {
			item.ExerciseID, item.ExerciseName = exercise.ID, exercise.Name
			entries = append(entries, item)
		}
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Exercises: exercises, Grid: buildGrid(exercises, entries)}, nil
}
