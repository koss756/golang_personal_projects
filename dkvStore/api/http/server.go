package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
)

type api struct {
	addr string
}

func (s *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello from server"))
}

func main() {
	api := &api{addr: ":8080"}

	mux := http.NewServeMux()

	srv := &http.Server{
		Addr:    api.addr,
		Handler: mux,
	}

	mux.HandleFunc("POST /command", api.handleCommand)

	srv.ListenAndServe()
}

func (a *api) handleCommand(w http.ResponseWriter, _ *http.Request) {
	wc, err := w.Write([]byte("Hello, world!\n"))

	if err != nil {
		slog.Error("Error writting response", "err", err)
		return
	}

	fmt.Print("%d bytes written\n", wc)
}
