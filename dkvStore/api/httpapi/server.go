// http/server.go
package httpapi

import (
	"net/http"

	"github.com/koss756/dkvStore/api/fault"
	"github.com/koss756/dkvStore/raft"
)

type Server struct {
	handler  raft.CommandHandler
	addr     string
	srv      *http.Server
	injector *fault.Injector
}

func NewServer(handler raft.CommandHandler, addr string, injector *fault.Injector) *Server {
	return &Server{
		handler:  handler,
		addr:     addr,
		injector: injector,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/command", s.handleCommand)
	mux.HandleFunc("/store", s.handleStore)
	mux.HandleFunc("GET /state", s.handleState)

	mux.HandleFunc("POST /fault", s.handleSetFault)
	mux.HandleFunc("DELETE /fault", s.handleClearFault)
	mux.HandleFunc("GET /fault", s.handleGetFault)

	s.srv = &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	return s.srv.ListenAndServe()
}
