package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/settings"
	"github.com/go-chi/chi/v5"
)

func (a *API) registerHealthRoutes(router chi.Router) {
	if a.health == nil {
		return
	}
	router.Get("/api/health/steps", a.stepsHistory)
	router.Post("/api/health/steps", a.ingestSteps)
	router.Get("/api/health/weight", a.weightHistory)
	router.Post("/api/health/weight", a.upsertWeight)
	router.Post("/api/health/weight/import", a.importWeight)
}

func (a *API) stepsHistory(response http.ResponseWriter, request *http.Request) {
	days, ok := queryDays(response, request)
	if !ok {
		return
	}
	result, err := a.health.StepsHistory(request.Context(), days, a.today())
	writeDomainResult(response, result, err)
}

func (a *API) ingestSteps(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
	if err != nil {
		writeError(response, http.StatusBadRequest, "Неверные данные шагов")
		return
	}
	parsed, err := health.ParseSteps(body, a.today())
	if err != nil {
		writeError(response, http.StatusBadRequest, "Неверные данные шагов")
		return
	}
	type parsedResponse struct {
		Source     string           `json:"source"`
		Days       []health.StepDay `json:"days"`
		TodaySteps *int             `json:"todaySteps"`
	}
	type resultResponse struct {
		OK        bool            `json:"ok"`
		Received  json.RawMessage `json:"received"`
		Parsed    *parsedResponse `json:"parsed"`
		SavedDays *int            `json:"savedDays"`
	}
	result := resultResponse{OK: true, Received: json.RawMessage("null")}
	if len(body) > 0 {
		result.Received = json.RawMessage(body)
	}
	if parsed != nil {
		saved, saveErr := a.health.UpsertSteps(request.Context(), *parsed)
		if saveErr != nil {
			writeDomainError(response, saveErr)
			return
		}
		result.Parsed = &parsedResponse{Source: parsed.Source, Days: parsed.Days, TodaySteps: parsed.TodaySteps()}
		result.SavedDays = &saved
	}
	writeJSON(response, http.StatusOK, result)
}

func (a *API) weightHistory(response http.ResponseWriter, request *http.Request) {
	days, ok := queryDays(response, request)
	if !ok {
		return
	}
	result, err := a.health.WeightHistory(request.Context(), days, a.today())
	writeDomainResult(response, result, err)
}

func (a *API) upsertWeight(response http.ResponseWriter, request *http.Request) {
	var body struct {
		WeightKg *float64 `json:"weightKg"`
		Date     *string  `json:"date"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	if body.WeightKg == nil {
		writeError(response, http.StatusBadRequest, "Validation failed")
		return
	}
	date := a.today().Format(time.DateOnly)
	if body.Date != nil {
		date = *body.Date
	}
	result, err := a.health.UpsertWeight(request.Context(), *body.WeightKg, date)
	writeDomainResult(response, result, err)
}

func (a *API) importWeight(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
	if err != nil {
		writeError(response, http.StatusBadRequest, "Неверный список значений веса")
		return
	}
	parsed, err := health.ParseWeightImport(body, a.today())
	if err != nil || parsed == nil {
		writeError(response, http.StatusBadRequest, "Неверный список значений веса")
		return
	}
	type importResponse struct {
		OK             bool     `json:"ok"`
		ReceivedDays   int      `json:"receivedDays"`
		SavedDays      int      `json:"savedDays"`
		LatestDate     *string  `json:"latestDate"`
		LatestWeightKg *float64 `json:"latestWeightKg"`
	}
	result := importResponse{OK: true, ReceivedDays: parsed.ReceivedDays}
	saved, saveErr := a.health.UpsertWeights(request.Context(), parsed.Days)
	if saveErr != nil {
		writeDomainError(response, saveErr)
		return
	}
	result.SavedDays = len(saved)
	if len(saved) > 0 {
		latest := saved[len(saved)-1]
		date, weight := latest.Date, latest.WeightKg
		result.LatestDate, result.LatestWeightKg = &date, &weight
	}
	writeJSON(response, http.StatusOK, result)
}

func (a *API) today() time.Time {
	zone := "Europe/Moscow"
	if a.settings != nil && a.settings.String(settings.TemporalZoneID) != "" {
		zone = a.settings.String(settings.TemporalZoneID)
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		location = time.UTC
	}
	now := time.Now().In(location)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
}

func queryDays(response http.ResponseWriter, request *http.Request) (int, bool) {
	raw := request.URL.Query().Get("days")
	if raw == "" {
		return 0, true
	}
	days, err := strconv.Atoi(raw)
	if err != nil {
		writeError(response, http.StatusBadRequest, "Invalid days")
		return 0, false
	}
	return days, true
}
