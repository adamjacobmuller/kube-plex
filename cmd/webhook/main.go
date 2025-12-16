package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/munnerz/kube-plex/pkg/signals"
	"github.com/munnerz/kube-plex/pkg/webhook"
)

func main() {
	var (
		port     int
		certFile string
		keyFile  string
	)

	flag.IntVar(&port, "port", 8443, "Webhook server port")
	flag.StringVar(&certFile, "cert", "/etc/webhook/certs/tls.crt", "TLS certificate file")
	flag.StringVar(&keyFile, "key", "/etc/webhook/certs/tls.key", "TLS key file")
	flag.Parse()

	// Allow environment variables to override flags
	if p := os.Getenv("WEBHOOK_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}
	if c := os.Getenv("TLS_CERT_FILE"); c != "" {
		certFile = c
	}
	if k := os.Getenv("TLS_KEY_FILE"); k != "" {
		keyFile = k
	}

	log.Printf("Starting kube-plex webhook server on port %d", port)

	// Load TLS certificates
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatalf("Failed to load TLS certificates: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// Create webhook handler
	handler := webhook.NewHandler()

	// Setup routes
	mux := http.NewServeMux()
	mux.Handle("/mutate", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:      fmt.Sprintf(":%d", port),
		Handler:   mux,
		TLSConfig: tlsConfig,
	}

	// Setup signal handler for graceful shutdown
	stopCh := signals.SetupSignalHandler()

	go func() {
		<-stopCh
		log.Println("Shutting down webhook server...")
		server.Close()
	}()

	log.Printf("Listening on :%d", port)
	if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start server: %v", err)
	}
}
