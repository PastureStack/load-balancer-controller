package logserver

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

const socketLocation = "/tmp/log.sock"

// Start exposes the legacy local log-level endpoint without retaining the
// abandoned rancher/log module.
func Start() error {
	if info, err := os.Lstat(socketLocation); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to replace non-socket %s", socketLocation)
		}
		if err := os.Remove(socketLocation); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	listener, err := net.Listen("unix", socketLocation)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/loglevel", logLevel)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("log-level server failed: %v", err)
		}
	}()
	log.Infof("Listening on %s", socketLocation)
	return nil
}

func logLevel(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	switch request.Method {
	case http.MethodGet:
		_, _ = fmt.Fprintln(response, log.GetLevel().String())
	case http.MethodPost:
		if err := request.ParseForm(); err != nil {
			http.Error(response, "invalid form", http.StatusBadRequest)
			return
		}
		level, err := log.ParseLevel(request.Form.Get("level"))
		if err != nil {
			http.Error(response, "invalid log level", http.StatusBadRequest)
			return
		}
		log.SetLevel(level)
		_, _ = fmt.Fprintln(response, "OK")
	default:
		response.Header().Set("Allow", "GET, POST")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
	}
}
