package main

import (
	"encoding/json"
	"log"
	"net/http"

	"bankrate-fde-case/internal/triage"
)

func main() {
	router := &triage.MemoryRouter{}
	audits := &triage.MemoryAuditStore{}
	service := triage.NewService(
		triage.RuleSafetyDetector{},
		triage.RuleClassifier{},
		triage.NewStaticPolicyRegistry(),
		triage.FakeKnowledgeBase{},
		triage.FakeDrafter{},
		router,
		audits,
		triage.BasicSanitizer{},
	)

	http.HandleFunc("/v1/triage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var intake triage.Intake
		if err := json.NewDecoder(r.Body).Decode(&intake); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		decision, err := service.Triage(r.Context(), intake)
		if err != nil {
			http.Error(w, "triage failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(decision)
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
