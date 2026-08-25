package main

import (
	"errors"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

const healthcheckPort = ":10241"

func startHealthcheck() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthcheck)
	server := &http.Server{
		Addr:              healthcheckPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	log.Info("Healthcheck handler is listening on ", healthcheckPort)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func healthcheck(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !lbc.IsHealthy() {
		log.Error("Healthcheck failed for LB controller")
		http.Error(response, "Healthcheck failed for LB controller", http.StatusInternalServerError)
		return
	}
	if !lbp.IsHealthy() {
		log.Error("Healthcheck failed for LB provider")
		http.Error(response, "Healthcheck failed for LB provider", http.StatusInternalServerError)
		return
	}
	_, _ = response.Write([]byte("OK"))
}
