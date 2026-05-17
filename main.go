package main

import (
	"context"
	"flag"
	"io"
	"log"

	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

//var logger = log.New(os.Stderr, "DEBUG: ", log.LstdFlags)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	<-ctx.Done()

	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	/*stdLogger := log.New(os.Stderr, "DEBUG: ", log.LstdFlags)
	stdLogger.Printf("Linko is running on http://localhost:%d\n", httpPort)

	f, err := os.OpenFile("linko.access.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		// handle the error
	}
	accessLogger := log.New(f, "INFO: ", log.LstdFlags)*/
	logger := initializeLogger()

	st, err := store.New(dataDir, stdLogger)
	if err != nil {
		stdLogger.Printf("failed to create store: %v\n", err)
		return 1
	}
	s := newServer(*st, httpPort, cancel, accessLogger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		stdLogger.Printf("failed to shutdown server: %v\n", err)
		return 1
	}
	if serverErr != nil {
		stdLogger.Printf("server error: %v\n", serverErr)
		return 1
	}
	stdLogger.Printf("Linko is shutting down\n")
	return 0
}

func initializeLogger() *log.Logger { //If a log file uri exists among environmental variables, write to that file in addition to stderr
	logFile := os.Getenv("LINKO_LOG_FILE")
	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			log.Fatalf("failed to open log file: %v", err)
		}
		multiWriter := io.MultiWriter(os.Stderr, file)
		logger := log.New(multiWriter, "INFO: ", log.LstdFlags)
		return logger
	} else {
		logger := log.New(os.Stderr, "INFO: ", log.LstdFlags)
		return logger
	}
}
