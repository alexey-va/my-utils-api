package report

import (
	"context"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/health"
)

type recordingHealthHistory struct {
	days  int
	today time.Time
}

func (h *recordingHealthHistory) StepsHistory(_ context.Context, days int, today time.Time) (health.StepsHistory, error) {
	h.days, h.today = days, today
	return health.StepsHistory{Days: []health.StepDay{{Date: today.Format(time.DateOnly), Steps: 12345}}}, nil
}

func (h *recordingHealthHistory) WeightHistory(_ context.Context, days int, today time.Time) (health.WeightHistory, error) {
	h.days, h.today = days, today
	return health.WeightHistory{Days: []health.WeightDay{{Date: today.Format(time.DateOnly), WeightKg: 81.2}}}, nil
}

type recordingWeeklyRenderer struct {
	stepsFrom, stepsTo   time.Time
	weightFrom, weightTo time.Time
	emptySteps           bool
}

func (r *recordingWeeklyRenderer) RenderSteps(_ []health.StepDay, from, to time.Time) ([]byte, error) {
	r.stepsFrom, r.stepsTo = from, to
	if r.emptySteps {
		return nil, nil
	}
	return []byte("steps"), nil
}

func (r *recordingWeeklyRenderer) RenderWeight(_ []health.WeightDay, from, to time.Time) ([]byte, error) {
	r.weightFrom, r.weightTo = from, to
	return []byte("weight"), nil
}

type recordingWeeklyMessenger struct {
	photos   [][]byte
	captions []string
}

func (*recordingWeeklyMessenger) SendHTMLMessage(context.Context, int64, string, string) (int, error) {
	return 1, nil
}

func (m *recordingWeeklyMessenger) SendPhoto(_ context.Context, _ int64, png []byte, caption string) error {
	m.photos = append(m.photos, png)
	m.captions = append(m.captions, caption)
	return nil
}

func TestSendWeeklyHealthReportRendersAndSendsTheSaturdayPair(t *testing.T) {
	t.Parallel()
	healthHistory := &recordingHealthHistory{}
	renderer := &recordingWeeklyRenderer{}
	messenger := &recordingWeeklyMessenger{}
	reportDate := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)

	if err := SendWeeklyHealthReport(context.Background(), healthHistory, renderer, messenger, 42, reportDate, 90); err != nil {
		t.Fatal(err)
	}
	if healthHistory.days != 90 || !healthHistory.today.Equal(reportDate) {
		t.Fatalf("history request = %d days ending %s", healthHistory.days, healthHistory.today)
	}
	wantFrom := reportDate.AddDate(0, 0, -89)
	if !renderer.stepsFrom.Equal(wantFrom) || !renderer.weightFrom.Equal(wantFrom) || !renderer.stepsTo.Equal(reportDate) || !renderer.weightTo.Equal(reportDate) {
		t.Fatalf("render ranges = steps %s..%s, weight %s..%s", renderer.stepsFrom, renderer.stepsTo, renderer.weightFrom, renderer.weightTo)
	}
	if len(messenger.photos) != 2 || string(messenger.photos[0]) != "steps" || string(messenger.photos[1]) != "weight" {
		t.Fatalf("photos = %#v", messenger.photos)
	}
	if messenger.captions[0] != "<b>Шаги · еженедельный отчёт</b>\nДо 17.09.2026 · последние 90 дней" || messenger.captions[1] != "<b>Вес · еженедельный отчёт</b>\nДо 17.09.2026 · последние 90 дней" {
		t.Fatalf("captions = %#v", messenger.captions)
	}
}

func TestSendWeeklyHealthReportRejectsEmptyChart(t *testing.T) {
	t.Parallel()
	messenger := &recordingWeeklyMessenger{}
	err := SendWeeklyHealthReport(
		context.Background(),
		&recordingHealthHistory{},
		&recordingWeeklyRenderer{emptySteps: true},
		messenger,
		42,
		time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC),
		90,
	)
	if err == nil {
		t.Fatal("expected empty chart error")
	}
	if len(messenger.photos) != 0 {
		t.Fatalf("sent %d photos after empty render", len(messenger.photos))
	}
}
