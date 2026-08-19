package report

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/workout"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type Renderer struct{}

func NewRenderer() *Renderer { return &Renderer{} }

type point struct {
	date  time.Time
	value float64
}

type tableRow struct {
	date  time.Time
	value string
}

func (r *Renderer) RenderSteps(values []health.StepDay, from, to time.Time) ([]byte, error) {
	points := make([]point, 0, len(values))
	for _, value := range values {
		date, err := time.Parse(time.DateOnly, value.Date)
		if err != nil {
			return nil, err
		}
		points = append(points, point{date: date, value: float64(value.Steps)})
	}
	return renderChart("Steps", "steps", points, from, to, color.RGBA{R: 91, G: 192, B: 235, A: 255}, latestStepRows(points, to))
}

func (r *Renderer) RenderWeight(values []health.WeightDay, from, to time.Time) ([]byte, error) {
	points := make([]point, 0, len(values))
	for _, value := range values {
		date, err := time.Parse(time.DateOnly, value.Date)
		if err != nil {
			return nil, err
		}
		points = append(points, point{date: date, value: value.WeightKg})
	}
	return renderChart("Body weight", "kg", points, from, to, color.RGBA{R: 255, G: 177, B: 66, A: 255}, latestWeightRows(points, to))
}

func (r *Renderer) RenderProgress(progress workout.Progress, recent int) ([]byte, error) {
	points := make([]point, 0, len(progress.Points))
	start := 0
	if recent > 0 && len(progress.Points) > recent {
		start = len(progress.Points) - recent
	}
	for _, value := range progress.Points[start:] {
		date, err := time.Parse(time.DateOnly, value.Date)
		if err != nil {
			return nil, err
		}
		points = append(points, point{date: date, value: value.WeightKg})
	}
	from, to := time.Now().AddDate(0, 0, -30), time.Now()
	if len(points) > 0 {
		from, to = points[0].date, points[len(points)-1].date
	}
	return renderChart(progress.Exercise.Name+" progress", "kg", points, from, to, color.RGBA{R: 117, G: 222, B: 154, A: 255}, nil)
}

func (r *Renderer) RenderOneRM(exercise string, weight float64, reps int, estimate float64) ([]byte, error) {
	canvas := newCanvas(700)
	drawLabel(canvas, 70, 90, "ONE REP MAX", color.RGBA{230, 235, 245, 255})
	drawLabel(canvas, 70, 145, exercise, color.RGBA{117, 222, 154, 255})
	drawLabel(canvas, 70, 250, fmt.Sprintf("ESTIMATE  %.1f kg", estimate), color.RGBA{255, 177, 66, 255})
	drawLabel(canvas, 70, 320, fmt.Sprintf("SOURCE    %.1f kg x %d", weight, reps), color.RGBA{205, 210, 225, 255})
	drawLabel(canvas, 70, 390, "FORMULA   Epley", color.RGBA{205, 210, 225, 255})
	zones := []struct {
		percent float64
		y       int
	}{{0.9, 500}, {0.8, 550}, {0.7, 600}}
	for _, zone := range zones {
		drawLabel(canvas, 70, zone.y, fmt.Sprintf("%3.0f%%       %.1f kg", zone.percent*100, estimate*zone.percent), color.RGBA{150, 160, 180, 255})
	}
	return encodePNG(canvas)
}

func renderChart(title, unit string, points []point, from, to time.Time, accent color.RGBA, rows []tableRow) ([]byte, error) {
	height := 700
	if rows != nil {
		height = max(1180, 800+len(rows)*44)
	}
	canvas := newCanvas(height)
	drawLabel(canvas, 70, 65, title, color.RGBA{230, 235, 245, 255})
	drawLabel(canvas, 70, 95, from.Format("02.01.2006")+" - "+to.Format("02.01.2006"), color.RGBA{140, 150, 170, 255})
	left, top, right, bottom := 110, 140, 1140, 610
	grid := color.RGBA{58, 64, 78, 255}
	for index := 0; index <= 5; index++ {
		y := top + (bottom-top)*index/5
		drawLine(canvas, left, y, right, y, grid, 1)
	}
	for index := 0; index <= 6; index++ {
		x := left + (right-left)*index/6
		drawLine(canvas, x, top, x, bottom, grid, 1)
	}
	if len(points) == 0 {
		drawLabel(canvas, 500, 390, "NO DATA", color.RGBA{140, 150, 170, 255})
		drawTable(canvas, rows, unit, accent)
		return encodePNG(canvas)
	}
	minimum, maximum := points[0].value, points[0].value
	for _, value := range points[1:] {
		minimum = math.Min(minimum, value.value)
		maximum = math.Max(maximum, value.value)
	}
	if maximum == minimum {
		margin := math.Max(1, maximum*0.05)
		minimum, maximum = minimum-margin, maximum+margin
	} else {
		margin := (maximum - minimum) * 0.12
		minimum, maximum = minimum-margin, maximum+margin
	}
	drawLabel(canvas, 55, top+8, formatValue(maximum)+" "+unit, color.RGBA{140, 150, 170, 255})
	drawLabel(canvas, 55, bottom, formatValue(minimum)+" "+unit, color.RGBA{140, 150, 170, 255})
	dateSpan := to.Sub(from).Hours() / 24
	if dateSpan < 1 {
		dateSpan = float64(max(1, len(points)-1))
	}
	previousX, previousY := 0, 0
	for index, value := range points {
		xRatio := value.date.Sub(from).Hours() / 24 / dateSpan
		if len(points) > 1 && (xRatio < 0 || xRatio > 1) {
			xRatio = float64(index) / float64(len(points)-1)
		}
		x := left + int(xRatio*float64(right-left))
		yRatio := (value.value - minimum) / (maximum - minimum)
		y := bottom - int(yRatio*float64(bottom-top))
		if index > 0 {
			drawLine(canvas, previousX, previousY, x, y, accent, 4)
		}
		drawCircle(canvas, x, y, 7, accent)
		previousX, previousY = x, y
	}
	drawLabel(canvas, left, 660, from.Format("02.01"), color.RGBA{140, 150, 170, 255})
	drawLabel(canvas, right-45, 660, to.Format("02.01"), color.RGBA{140, 150, 170, 255})
	drawTable(canvas, rows, unit, accent)
	return encodePNG(canvas)
}

func drawTable(canvas *image.RGBA, rows []tableRow, unit string, accent color.RGBA) {
	if rows == nil {
		return
	}
	top := 720
	drawLabel(canvas, 70, top, "LATEST VALUES", color.RGBA{230, 235, 245, 255})
	drawLabel(canvas, 70, top+38, "DATE", color.RGBA{140, 150, 170, 255})
	drawLabel(canvas, 980, top+38, strings.ToUpper(unit), accent)
	for index, row := range rows {
		y := top + 70 + index*44
		drawLine(canvas, 70, y-22, 1130, y-22, color.RGBA{58, 64, 78, 255}, 1)
		drawLabel(canvas, 70, y, row.date.Format("02.01.2006"), color.RGBA{205, 210, 225, 255})
		drawLabel(canvas, 980, y, row.value, color.RGBA{230, 235, 245, 255})
	}
}

func latestStepRows(points []point, to time.Time) []tableRow {
	byDate := make(map[string]float64, len(points))
	for _, value := range points {
		byDate[value.date.Format(time.DateOnly)] = value.value
	}
	rows := make([]tableRow, 0, 10)
	for offset := 0; offset < 10; offset++ {
		date := to.AddDate(0, 0, -offset)
		value := "-"
		if steps, ok := byDate[date.Format(time.DateOnly)]; ok {
			value = formatGroupedInteger(int64(math.Round(steps)))
		}
		rows = append(rows, tableRow{date: date, value: value})
	}
	return rows
}

func latestWeightRows(points []point, to time.Time) []tableRow {
	byDate := make(map[string]point, len(points))
	for _, value := range points {
		if !value.date.After(to) {
			byDate[value.date.Format(time.DateOnly)] = value
		}
	}
	values := make([]point, 0, len(byDate))
	for _, value := range byDate {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].date.After(values[j].date) })
	values = values[:min(10, len(values))]
	rows := make([]tableRow, 0, len(values))
	for _, value := range values {
		rows = append(rows, tableRow{date: value.date, value: fmt.Sprintf("%.1f kg", value.value)})
	}
	return rows
}

func formatGroupedInteger(value int64) string {
	raw := strconv.FormatInt(value, 10)
	for index := len(raw) - 3; index > 0; index -= 3 {
		raw = raw[:index] + " " + raw[index:]
	}
	return raw
}

func newCanvas(height int) *image.RGBA {
	canvas := image.NewRGBA(image.Rect(0, 0, 1200, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{29, 34, 45, 255}}, image.Point{}, draw.Src)
	for y := 0; y < height; y += 4 {
		for x := (y / 4) % 4; x < 1200; x += 16 {
			canvas.SetRGBA(x, y, color.RGBA{31, 36, 48, 255})
		}
	}
	return canvas
}

func drawLabel(canvas *image.RGBA, x, y int, text string, colour color.Color) {
	drawer := &font.Drawer{Dst: canvas, Src: image.NewUniform(colour), Face: basicfont.Face7x13, Dot: fixed.P(x, y)}
	drawer.DrawString(text)
}

func drawLine(canvas *image.RGBA, x0, y0, x1, y1 int, colour color.RGBA, width int) {
	dx, dy := int(math.Abs(float64(x1-x0))), -int(math.Abs(float64(y1-y0)))
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		drawCircle(canvas, x0, y0, max(0, width/2), colour)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func drawCircle(canvas *image.RGBA, cx, cy, radius int, colour color.RGBA) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= radius*radius {
				canvas.SetRGBA(cx+x, cy+y, colour)
			}
		}
	}
}

func encodePNG(canvas image.Image) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&buffer, canvas); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func formatValue(value float64) string { return strconv.FormatFloat(value, 'f', 1, 64) }
