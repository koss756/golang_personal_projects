// http/server.go
package httpapi

import (
	"net/http"

	"github.com/koss756/dkvStore/raft"
)

type Server struct {
	handler raft.CommandHandler
	addr    string
	srv     *http.Server
}

func NewServer(handler raft.CommandHandler, addr string) *Server {
	return &Server{
		handler: handler,
		addr:    addr,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/command", s.handleCommand)
	// mux.HandleFunc("/status", s.handleStatus)

	s.srv = &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	return s.srv.ListenAndServe()
}
