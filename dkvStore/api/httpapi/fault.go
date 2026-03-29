package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/koss756/dkvStore/api/fault"
)

type faultRequest struct {
	LatencyMs   int     `json:"latency_ms"`
	DropRate    float64 `json:"drop_rate"`
	Partitioned bool    `json:"partitioned"`
	Target      string  `json:"target"`
}

func parseFaultTarget(r *http.Request, body *faultRequest) string {
	if q := r.URL.Query().Get("target"); q != "" {
		return q
	}
	return body.Target
}

func (s *Server) handleSetFault(w http.ResponseWriter, r *http.Request) {
	var body faultRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg := fault.Config{
		LatencyMs:   body.LatencyMs,
		DropRate:    body.DropRate,
		Partitioned: body.Partitioned,
	}
	target := parseFaultTarget(r, &body)
	if target == "" {
		s.injector.Set(cfg)
	} else {
		s.injector.SetForTarget(target, cfg)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClearFault(w http.ResponseWriter, r *http.Request) {
	if target := r.URL.Query().Get("target"); target != "" {
		s.injector.ClearTarget(target)
	} else {
		s.injector.Reset()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetFault(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.injector.Snapshot())
}
