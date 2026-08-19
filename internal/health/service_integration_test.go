package health

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBulkImportsAreAtomic(t *testing.T) {
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
	userID, err := service.localUserID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM health_steps WHERE user_id=$1::uuid AND step_date BETWEEN '2037-01-01' AND '2037-01-02'`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM health_body_weight WHERE user_id=$1::uuid AND weight_date BETWEEN '2037-01-01' AND '2037-01-02'`, userID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM health_steps WHERE user_id=$1::uuid AND step_date BETWEEN '2037-01-01' AND '2037-01-02'`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM health_body_weight WHERE user_id=$1::uuid AND weight_date BETWEEN '2037-01-01' AND '2037-01-02'`, userID)
	}()

	if _, err := service.UpsertSteps(ctx, ParsedSteps{Days: []StepDay{{Date: "2037-01-01", Steps: 1000}, {Date: "2037-01-02", Steps: -1}}}); err == nil {
		t.Fatal("negative second row should fail the steps batch")
	}
	var steps int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM health_steps WHERE user_id=$1::uuid AND step_date='2037-01-01'`, userID).Scan(&steps); err != nil || steps != 0 {
		t.Fatalf("partial steps rows = %d, err=%v", steps, err)
	}

	if _, err := service.UpsertWeights(ctx, []WeightDay{{Date: "2037-01-01", WeightKg: 80}, {Date: "2037-01-02", WeightKg: 500}}); err == nil {
		t.Fatal("invalid second row should fail the weight batch")
	}
	var weights int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM health_body_weight WHERE user_id=$1::uuid AND weight_date='2037-01-01'`, userID).Scan(&weights); err != nil || weights != 0 {
		t.Fatalf("partial weight rows = %d, err=%v", weights, err)
	}
}
