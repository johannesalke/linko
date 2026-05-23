package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"slices"
	//"io"
	"boot.dev/linko/internal/build"
	"boot.dev/linko/internal/linkoerr"
	"boot.dev/linko/internal/store"
	"errors"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	pkgerr "github.com/pkg/errors"
	"gopkg.in/natefinch/lumberjack.v2"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
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
	shutdownTracing, err := initTracing(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize tracing: %v\n", err)
		return 1
	}
	defer func() {
		if err := shutdownTracing(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to shut down tracing: %v\n", err)
		}
	}()

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
	env := os.Getenv("ENV")
	hostname, _ := os.Hostname()
	if logFile != "" {
		/*file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: %v", err)
		}
		bufferedFile := bufio.NewWriterSize(file, 8192)*/
		lumberjackLogger := &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    1,
			MaxAge:     28,
			MaxBackups: 10,
			LocalTime:  false,
			Compress:   true,
		}
		closer := func() error {
			err := lumberjackLogger.Close()
			return err
		}

		infoHandler := slog.NewJSONHandler(lumberjackLogger, &slog.HandlerOptions{
			Level:       slog.LevelInfo,
			ReplaceAttr: replaceAttr,
		})

		debugHandler := tint.NewHandler(os.Stderr, &tint.Options{
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceAttr,
			NoColor:     !(isatty.IsCygwinTerminal(os.Stdout.Fd()) || isatty.IsTerminal(os.Stdout.Fd())),
		})

		logger := slog.New(slog.NewMultiHandler(
			debugHandler,
			infoHandler,
		)).With(
			slog.String("git_sha", build.GitSHA),
			slog.String("build_time", build.BuildTime),
			slog.String("env", env),
			slog.String("hostname", hostname),
		)

		//multiWriter := io.MultiWriter(os.Stderr, bufferedFile)
		//logger := slog.New(slog.NewTextHandler(multiWriter, nil))
		//logger := log.New(multiWriter, "", log.LstdFlags)
		return logger, closer, nil
	} else {
		logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceAttr,
			NoColor:     !(isatty.IsCygwinTerminal(os.Stderr.Fd()) || isatty.IsTerminal(os.Stderr.Fd())),
		})).With(
			slog.String("git_sha", build.GitSHA),
			slog.String("build_time", build.BuildTime),
			slog.String("env", env),
			slog.String("hostname", hostname),
		)
		closer := func() error { return nil }
		return logger, closer, nil
	}
}

type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}

type multiError interface {
	error
	Unwrap() []error
}

func errorAttrs(err error) []slog.Attr {
	attrs := []slog.Attr{
		{Key: "message", Value: slog.StringValue(err.Error())},
	}
	attrs = append(attrs, linkoerr.Attrs(err)...)
	if stackErr, ok := errors.AsType[stackTracer](err); ok {
		attrs = append(attrs, slog.Attr{
			Key:   "stack_trace",
			Value: slog.StringValue(fmt.Sprintf("%+v", stackErr.StackTrace())),
		})
	}
	return attrs
}

var sensitiveKeys = []string{"user", "password", "key", "apikey", "secret", "pin", "creditcardno"}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if slices.Contains(sensitiveKeys, a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}
	if a.Value.Kind() == slog.KindString {
		if u, err := url.Parse(a.Value.String()); err == nil {
			if _, hasPassword := u.User.Password(); hasPassword {
				u.User = url.UserPassword(u.User.Username(), "[REDACTED]")
				return slog.String(a.Key, u.String())
			}
		}
	}
	if a.Key == "error" {
		err, ok := a.Value.Any().(error)
		if !ok {
			return a
		}
		if multiErr, ok := errors.AsType[multiError](err); ok {
			var errAttrs []slog.Attr
			for i, e := range multiErr.Unwrap() {
				errAttrs = append(errAttrs, slog.GroupAttrs(fmt.Sprintf("error_%d", i+1), errorAttrs(e)...))
			}
			return slog.GroupAttrs("errors", errAttrs...)
		}

		return slog.GroupAttrs("error", errorAttrs(err)...)
	}
	return a
}
