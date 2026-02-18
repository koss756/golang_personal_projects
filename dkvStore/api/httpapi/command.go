package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/koss756/dkvStore/raft"
)

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	var cmd raft.Command
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.handler.SubmitCommand(r.Context(), cmd); err != nil {
		leaderIdStr := err.Error()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict) // 409 is better than 500

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   false,
			"leader_id": leaderIdStr,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}
