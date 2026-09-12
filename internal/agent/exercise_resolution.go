package agent

import (
	"fmt"
	"strings"
)

// ExerciseCandidate is the small common shape shared by real and sandbox
// exercise lists. Mutating callers should use the returned ID as the stable
// identity instead of passing a display name to a second lookup.
type ExerciseCandidate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MuscleGroup string `json:"muscleGroup,omitempty"`
}

const (
	ExerciseResolutionUnknownID = "unknown_id"
	ExerciseResolutionNotFound  = "not_found"
	ExerciseResolutionAmbiguous = "ambiguous"
)

// ExerciseResolutionError carries canonical candidates so an interpreter can
// ask one precise follow-up and then retry with exercise_id.
type ExerciseResolutionError struct {
	Kind        string
	RequestedID string
	Requested   string
	Candidates  []ExerciseCandidate
}

func (e *ExerciseResolutionError) Error() string {
	switch e.Kind {
	case ExerciseResolutionUnknownID:
		return fmt.Sprintf("упражнение с id %q не найдено среди доступных; выбери один из: %s", e.RequestedID, formatExerciseCandidates(e.Candidates))
	case ExerciseResolutionAmbiguous:
		return fmt.Sprintf("упражнение %q неоднозначно; выбери canonical id: %s", e.Requested, formatExerciseCandidates(e.Candidates))
	default:
		if strings.TrimSpace(e.Requested) == "" {
			return "упражнение не указано"
		}
		return fmt.Sprintf("упражнение %q не найдено; доступные: %s", e.Requested, formatExerciseCandidates(e.Candidates))
	}
}

// ResolveExercise resolves a canonical ID first. If no ID is supplied, an
// exact normalized name wins over substring matches; every non-unique result
// is returned as an ambiguity with IDs and names for the next turn.
func ResolveExercise(requestedID, requestedName string, candidates []ExerciseCandidate) (ExerciseCandidate, error) {
	candidates = validExerciseCandidates(candidates)
	requestedID = strings.TrimSpace(requestedID)
	requestedName = strings.TrimSpace(requestedName)
	if requestedID != "" {
		matches := make([]ExerciseCandidate, 0, 1)
		for _, candidate := range candidates {
			if strings.EqualFold(candidate.ID, requestedID) {
				matches = append(matches, candidate)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		return ExerciseCandidate{}, &ExerciseResolutionError{Kind: ExerciseResolutionUnknownID, RequestedID: requestedID, Requested: requestedName, Candidates: candidates}
	}

	normalized := normalizeExerciseName(requestedName)
	if normalized == "" {
		return ExerciseCandidate{}, &ExerciseResolutionError{Kind: ExerciseResolutionNotFound, Requested: requestedName, Candidates: candidates}
	}
	exact := make([]ExerciseCandidate, 0, 1)
	for _, candidate := range candidates {
		if normalizeExerciseName(candidate.Name) == normalized {
			exact = append(exact, candidate)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return ExerciseCandidate{}, &ExerciseResolutionError{Kind: ExerciseResolutionAmbiguous, Requested: requestedName, Candidates: exact}
	}

	fuzzy := make([]ExerciseCandidate, 0, 1)
	for _, candidate := range candidates {
		if strings.Contains(normalizeExerciseName(candidate.Name), normalized) {
			fuzzy = append(fuzzy, candidate)
		}
	}
	if len(fuzzy) == 1 {
		return fuzzy[0], nil
	}
	if len(fuzzy) > 1 {
		return ExerciseCandidate{}, &ExerciseResolutionError{Kind: ExerciseResolutionAmbiguous, Requested: requestedName, Candidates: fuzzy}
	}
	return ExerciseCandidate{}, &ExerciseResolutionError{Kind: ExerciseResolutionNotFound, Requested: requestedName, Candidates: candidates}
}

func normalizeExerciseName(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func validExerciseCandidates(candidates []ExerciseCandidate) []ExerciseCandidate {
	result := make([]ExerciseCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.ID = strings.TrimSpace(candidate.ID)
		candidate.Name = strings.TrimSpace(candidate.Name)
		if candidate.ID == "" || candidate.Name == "" {
			continue
		}
		result = append(result, candidate)
	}
	return result
}

func formatExerciseCandidates(candidates []ExerciseCandidate) string {
	if len(candidates) == 0 {
		return "нет доступных упражнений"
	}
	parts := make([]string, len(candidates))
	for index, candidate := range candidates {
		parts[index] = fmt.Sprintf("[%s] %s", candidate.ID, candidate.Name)
	}
	return strings.Join(parts, "; ")
}
