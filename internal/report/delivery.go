package report

import (
	"context"
	"fmt"
	"strings"

	"github.com/alexey-va/my-utils-api/internal/workout"
)

type Messenger interface {
	SendHTMLMessage(context.Context, int64, string, string) (int, error)
	SendPhoto(context.Context, int64, []byte, string) error
}

type Delivery struct {
	workout   *workout.Service
	renderer  *Renderer
	messenger Messenger
}

func NewDelivery(workoutService *workout.Service, renderer *Renderer, messenger Messenger) *Delivery {
	return &Delivery{workout: workoutService, renderer: renderer, messenger: messenger}
}

func (d *Delivery) SendRichMessage(ctx context.Context, chatID int64, text, buttons string) error {
	_, err := d.messenger.SendHTMLMessage(ctx, chatID, text, buttons)
	return err
}

func (d *Delivery) SendProgressChart(ctx context.Context, chatID int64, exerciseName string, recent int) error {
	exercise, err := d.findExercise(ctx, exerciseName)
	if err != nil {
		return err
	}
	progress, err := d.workout.Progress(ctx, exercise.ID)
	if err != nil {
		return err
	}
	png, err := d.renderer.RenderProgress(progress, recent)
	if err != nil {
		return err
	}
	return d.messenger.SendPhoto(ctx, chatID, png, fmt.Sprintf("<b>%s · прогресс</b>\nПоследние %d сессий", exercise.Name, recent))
}

func (d *Delivery) SendOneRM(ctx context.Context, chatID int64, exerciseName string, weight float64, reps int, estimate float64) error {
	png, err := d.renderer.RenderOneRM(exerciseName, weight, reps, estimate)
	if err != nil {
		return err
	}
	return d.messenger.SendPhoto(ctx, chatID, png, fmt.Sprintf("<b>%s · оценка 1ПМ</b>\n%.1f кг", exerciseName, estimate))
}

func (d *Delivery) findExercise(ctx context.Context, name string) (workout.Exercise, error) {
	exercises, err := d.workout.ListExercises(ctx)
	if err != nil {
		return workout.Exercise{}, err
	}
	needle := strings.ToLower(strings.TrimSpace(name))
	matches := []workout.Exercise{}
	for _, exercise := range exercises {
		candidate := strings.ToLower(exercise.Name)
		if candidate == needle || strings.Contains(candidate, needle) || strings.Contains(needle, candidate) {
			matches = append(matches, exercise)
		}
	}
	if len(matches) == 0 {
		return workout.Exercise{}, fmt.Errorf("упражнение %q не найдено", name)
	}
	if len(matches) > 1 {
		return workout.Exercise{}, fmt.Errorf("упражнение %q неоднозначно", name)
	}
	return matches[0], nil
}
