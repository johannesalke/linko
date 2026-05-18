package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	//"io"
	"errors"
	pkgerr "github.com/pkg/errors"
	"log/slog"
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
	/*
		ctx, stop := signal.NotifyContext(
			context.Background(),
			os.Interrupt,
			syscall.SIGTERM,
		)
		defer stop()

		<-ctx.Done()
	*/

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
	logFile := os.Getenv("LINKO_LOG_FILE")
	logger, loggerCloser, err := initializeLogger(logFile)
	if err != nil {
		fmt.Print(err)
		return 1
	}
	logger.Debug(fmt.Sprintf("Linko is running on http://localhost:%d\n", httpPort))
	defer func() {
		if err := loggerCloser(); err != nil {
			fmt.Print(err)
		}
	}()

	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Error("failed to create store", "error", err)
		return 1
	}
	s := newServer(*st, httpPort, cancel, logger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error("failed to shutdown server", "error", err)
		return 1
	}
	if serverErr != nil {
		logger.Error("server error", "error", serverErr)
		return 1
	}

	logger.Debug("Linko is shutting down")
	return 0
}

type closeFunc func() error

func initializeLogger(logFile string) (*slog.Logger, closeFunc, error) { //If a log file uri exists among environmental variables, write to that file in addition to stderr

	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: %v", err)
		}
		bufferedFile := bufio.NewWriterSize(file, 8192)
		closer := func() error {
			err := bufferedFile.Flush()
			return err
		}
		infoHandler := slog.NewJSONHandler(bufferedFile, &slog.HandlerOptions{
			Level:       slog.LevelInfo,
			ReplaceAttr: replaceAttr,
		})
		debugHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceAttr,
		})
		logger := slog.New(slog.NewMultiHandler(
			debugHandler,
			infoHandler,
		))

		//multiWriter := io.MultiWriter(os.Stderr, bufferedFile)
		//logger := slog.New(slog.NewTextHandler(multiWriter, nil))
		//logger := log.New(multiWriter, "", log.LstdFlags)
		return logger, closer, nil
	} else {
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceAttr,
		}))
		closer := func() error { return nil }
		return logger, closer, nil
	}
}

type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == "error" {
		err, ok := a.Value.Any().(error)
		if !ok {
			return a
		}
		if stackErr, ok := errors.AsType[stackTracer](err); ok {
			return slog.GroupAttrs("error", slog.Attr{
				Key:   "message",
				Value: slog.StringValue(stackErr.Error()),
			}, slog.Attr{
				Key:   "stack_trace",
				Value: slog.StringValue(fmt.Sprintf("%+v", stackErr.StackTrace())),
			})
		}
	}
	return a
}
