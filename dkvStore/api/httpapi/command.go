package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/koss756/dkvStore/kvstore"
)

// Command is the JSON shape for /command; only set/delete are replicated.
type Command struct {
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

func SerializeCommand(cmd Command) ([]byte, error) {
	return kvstore.EncodeCommand(kvstore.Command{
		Op:    cmd.Op,
		Key:   cmd.Key,
		Value: cmd.Value,
	})
}

func (s *Server) handleStore(w http.ResponseWriter, r *http.Request) {
	store := s.handler.GetStore()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(store)
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Restore the body so Decode can still read it
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	var cmd Command
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if cmd.Op != kvstore.OpSet && cmd.Op != kvstore.OpDelete {
		http.Error(w, `op must be "set" or "delete"`, http.StatusBadRequest)
		return
	}

	cmd_byte, err := SerializeCommand(cmd)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.handler.SubmitCommand(r.Context(), cmd_byte); err != nil {
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
