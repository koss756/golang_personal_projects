package httpapi

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"io"
	"log"
	"net/http"
)

type Command struct {
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

func SerializeCommand(cmd Command) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(cmd)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("Raw request body: %s", string(body))

	// Restore the body so Decode can still read it
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	var cmd Command
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
