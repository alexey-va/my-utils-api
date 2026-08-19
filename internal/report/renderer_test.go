package report

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/health"
)

func TestWeeklyRenderersProduceReadablePNG(t *testing.T) {
	t.Parallel()
	renderer := NewRenderer()
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		png  []byte
		err  error
	}{
		func() struct {
			name string
			png  []byte
			err  error
		} {
			data, err := renderer.RenderSteps([]health.StepDay{{Date: "2026-08-19", Steps: 9000}, {Date: "2026-08-20", Steps: 12000}}, from, to)
			return struct {
				name string
				png  []byte
				err  error
			}{"steps", data, err}
		}(),
		func() struct {
			name string
			png  []byte
			err  error
		} {
			data, err := renderer.RenderWeight([]health.WeightDay{{Date: "2026-08-19", WeightKg: 82.5}, {Date: "2026-08-20", WeightKg: 82.1}}, from, to)
			return struct {
				name string
				png  []byte
				err  error
			}{"weight", data, err}
		}(),
	}
	for _, test := range tests {
		if test.err != nil {
			t.Fatal(test.err)
		}
		image, err := png.Decode(bytes.NewReader(test.png))
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if image.Bounds().Dx() < 800 || image.Bounds().Dy() < 1_100 || len(test.png) < 5_000 {
			t.Fatalf("%s PNG too small: bounds=%v bytes=%d", test.name, image.Bounds(), len(test.png))
		}
	}
}

func TestWeeklyTablesKeepStepsCalendarAndWeightMeasurements(t *testing.T) {
	t.Parallel()
	to := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	stepRows := latestStepRows([]point{{date: to.AddDate(0, 0, -1), value: 12345}}, to)
	if len(stepRows) != 10 || stepRows[0].value != "-" || stepRows[1].value != "12 345" {
		t.Fatalf("step rows = %#v", stepRows)
	}
	weights := make([]point, 0, 12)
	for index := 0; index < 12; index++ {
		weights = append(weights, point{date: to.AddDate(0, 0, -index*2), value: 80 + float64(index)/10})
	}
	weightRows := latestWeightRows(weights, to)
	if len(weightRows) != 10 || weightRows[0].date != to || weightRows[0].value != "80.0 kg" || !weightRows[9].date.Equal(to.AddDate(0, 0, -18)) {
		t.Fatalf("weight rows = %#v", weightRows)
	}
}
