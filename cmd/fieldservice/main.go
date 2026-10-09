package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/infrai-examples/field-service-staged-rollout/internal/fieldservice"
	"github.com/infrai-examples/field-service-staged-rollout/internal/infrai"
)

type server struct {
	client    *infrai.Client
	processor *fieldservice.Processor
	publicURL string
}

func main() {
	baseURL := envOr("INFRAI_BASE_URL", infrai.DefaultBaseURL)
	client, err := infrai.New(baseURL, os.Getenv("INFRAI_API_KEY"), nil)
	if err != nil {
		log.Fatal(err)
	}
	s := &server{
		client:    client,
		processor: fieldservice.NewProcessor(client, nil),
		publicURL: strings.TrimRight(envOr("PUBLIC_URL", "http://localhost:8080"), "/"),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /work-orders", s.createWorkOrder)
	mux.HandleFunc("POST /rollouts", s.scheduleRollout)
	mux.HandleFunc("POST /rollout-steps/{step}", s.advanceRollout)
	log.Printf("field-service listening on %s", envOr("ADDR", ":8080"))
	log.Fatal(http.ListenAndServe(envOr("ADDR", ":8080"), mux))
}

func (s *server) createWorkOrder(w http.ResponseWriter, r *http.Request) {
	var input fieldservice.Intake
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	order, err := s.processor.Process(r.Context(), input)
	if err != nil {
		writeClientError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, order)
}

type rolloutRequest struct {
	Steps []struct {
		Name     string `json:"name"`
		CronExpr string `json:"cron_expr"`
	} `json:"steps"`
}

func (s *server) scheduleRollout(w http.ResponseWriter, r *http.Request) {
	var input rolloutRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.Steps) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "steps are required"})
		return
	}
	type scheduled struct {
		Name  string `json:"name"`
		JobID string `json:"job_id"`
	}
	jobs := make([]scheduled, 0, len(input.Steps))
	for _, step := range input.Steps {
		if step.Name == "" || step.CronExpr == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "each step needs name and cron_expr"})
			return
		}
		taskURL := s.publicURL + "/rollout-steps/" + step.Name
		idempotencyKey := fieldservice.FeatureKey + ":schedule:" + step.Name
		job, err := s.client.Schedule(r.Context(), step.CronExpr, taskURL, idempotencyKey)
		if err != nil {
			writeClientError(w, err)
			return
		}
		jobs = append(jobs, scheduled{Name: step.Name, JobID: job.JobID})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"feature_key": fieldservice.FeatureKey, "jobs": jobs})
}

func (s *server) advanceRollout(w http.ResponseWriter, r *http.Request) {
	step := r.PathValue("step")
	if step == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "step is required"})
		return
	}
	if err := s.client.AdvanceRollout(r.Context(), fieldservice.FeatureKey, fieldservice.FeatureKey+":advance:"+step); err != nil {
		writeClientError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"feature_key": fieldservice.FeatureKey, "step": step, "status": "advanced"})
}

func writeClientError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	var apiErr *infrai.APIError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
		status = apiErr.Status
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
