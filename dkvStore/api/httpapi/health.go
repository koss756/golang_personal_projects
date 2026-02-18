package httpapi

import "net/http"

func (s *Server) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK!"))
}
