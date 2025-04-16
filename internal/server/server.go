package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/saltosystems-internal/x/log"
	pkgserver "github.com/saltosystems-internal/x/server"
)

//go:embed static/index.html
//go:embed static/actualizaciones.html
//go:embed static/images/*
var staticFiles embed.FS

type Server struct {
	s      *pkgserver.GroupServer
	logger log.Logger
	cancel context.CancelFunc
}

type UpdateStatus struct {
	UpdateAvailable int `json:"update_available"`
	UpdateRequested int `json:"update_requested"`
}

var (
	jsonFilePath = "/home/sormazabal/src/SALTO-client-linux/update_status.json"
	updateStatus UpdateStatus
	updateMutex  sync.Mutex
)

// readUpdateStatus examinates that update_status.json exists and that can be poperly parsed
func readUpdateStatus() {
	updateMutex.Lock()
	defer updateMutex.Unlock()

	file, err := os.ReadFile(jsonFilePath)
	if err != nil {
		fmt.Println("⚠️ Could not read update status file, using default (0)")
		return
	}

	err = json.Unmarshal(file, &updateStatus)
	if err != nil {
		fmt.Println("⚠️ Could not parse update status JSON, using default (0)")
	}
}

// checkUpdateHandler is an HTTP hanfler function in GO that responds to an HTTP request with JSON data
func checkUpdateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updateStatus)
}

// runUpdaterHandler is an HTTP handler that initiated an update process when it retrieves a POST request
func runUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Println("⚙️ Running update process...")
	setUpdateRequestedStatus(1)

	// handleShutdown waits for a termination signal and shuts down the server
	// syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	// Restart the application (or notify an external service manager)
}

func periodicUpdateCheck(ctx context.Context) {

	// A ticker is used to perform a specific action at a specific interval
	// It repeatedly sends a signal on a channel ticker.C
	ticker := time.NewTicker(1 * time.Second)

	// This ensures that the ticker stops when the function exists, preventing memory leacks
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			readUpdateStatus()
			if updateStatus.UpdateAvailable == 1 {
				fmt.Println("🔄 Update available! Notifying frontend.")
			}
		case <-ctx.Done():
			fmt.Println("🛑 Stopping periodic update check...")
			return
		}
	}
}

// corsMiddleware enables CORS (Cross-origin Resource Sharing) for an HTTP server.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Setting "update_requested" to a value
func setUpdateRequestedStatus(value int) error {
	updateMutex.Lock()
	defer updateMutex.Unlock()

	updateStatus.UpdateRequested = value
	file, err := json.MarshalIndent(updateStatus, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(jsonFilePath, file, 0644)
}

// NewServer brings up the server
func NewServer(cfg *Config, logger log.Logger) (*Server, error) {
	var (
		servers        []pkgserver.Server
		httpServerOpts []pkgserver.HTTPServerOption
	)

	if cfg.HTTPAddr == "" {
		return nil, errors.New("invalid config: HTTPAddr missing")
	}
	// The mux variable in this code is an HTTP request multiplexer created using http.NewServeMux().
	// It is responsible for routing incoming HTTP requests to the correct handler functions based on the request URL.
	mux := http.NewServeMux()
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, err
	}

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/nebula", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data, err := staticFiles.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "Index file not found", http.StatusInternalServerError)
			return
		}
		w.Write(data)
	})

	mux.HandleFunc("/check-update", checkUpdateHandler)
	mux.HandleFunc("/run-update", runUpdateHandler)

	wrappedMux := corsMiddleware(mux)
	ctx, cancel := context.WithCancel(context.Background())
	go periodicUpdateCheck(ctx)

	httpServerOpts = append(httpServerOpts, pkgserver.WithRoutes(
		&pkgserver.Route{Pattern: "/", Handler: wrappedMux},
	))
	httpServer, err := pkgserver.NewHTTPServer(cfg.HTTPAddr, httpServerOpts...)
	if err != nil {
		cancel()
		return nil, err
	}
	servers = append(servers, httpServer)

	s, err := pkgserver.NewGroupServer(context.Background(), pkgserver.WithServers(servers))
	if err != nil {
		cancel()
		return nil, err
	}

	return &Server{s: s, logger: logger, cancel: cancel}, nil
}

// It runs the server
func (s *Server) Run() error {
	fmt.Println("🚀 Server started...")
	return s.s.Run(context.Background())
}

// Shutdown shutdowns the server
func (s *Server) Shutdown() {
	fmt.Println("🛑 Shutting down server...")
	s.cancel()
	time.Sleep(1 * time.Second)
	fmt.Println("✅ Server stopped.")
}
