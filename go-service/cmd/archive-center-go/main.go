// archive-center-go is the entry point for the Archive Center 2.0 shadow service.
// It starts an HTTP server on a non-conflicting port with shadow-only defaults.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/risulongmemory/archive-center-go/internal/config"
	"github.com/risulongmemory/archive-center-go/internal/httpapi"
)

// AC_LOG_LEVEL: debug | info | warn | error (기본 info). 포크 추가.
func parseLogLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// 응답 status/바이트 수를 집계하려면 ResponseWriter를 감싸야 한다.
// Flush는 원본에 위임한다 (감싸면서 http.Flusher를 잃으면 스트리밍이 끊긴다).
type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// 모든 요청을 한 줄씩 남긴다. 4xx는 WARN, 5xx는 ERROR로 올려서 등급으로 걸러낼 수 있게 한다.
func withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400:
			level = slog.LevelWarn
		}
		slog.Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: parseLogLevel(os.Getenv("AC_LOG_LEVEL")),
	}))
	// 패키지 전역 slog를 쓰는 곳(요청 로그, internal/httpapi 진단 로그)도 같은 설정을 타게 한다.
	slog.SetDefault(logger)
	appCtx, cancelApp := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelApp()
	if httpapi.ConfigureOutboundDNSServers(os.Getenv("AC_DNS_SERVERS")) {
		logger.Info("configured outbound dns override")
	}

	cfg := config.Load()
	logger.Info("loaded config", "config", cfg.String())

	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(1)
	}

	if cfg.Mode != config.ModeShadow {
		if !cfg.IsLiveCutoverAllowed() {
			logger.Error("live/cutover mode is not allowed with this configuration", "config", cfg.String())
			os.Exit(1)
		}
		logger.Info("product runtime mode enabled", "mode", cfg.Mode, "store_mode", cfg.StoreMode)
	}

	mux := http.NewServeMux()
	server := httpapi.NewServer(cfg)
	var requestedExitCode atomic.Int32
	if managedUpdateLauncherAuthorized(cfg) {
		server.RequestShutdown = func(exitCode int) {
			if exitCode == httpapi.UpdateApplyExitCode && requestedExitCode.CompareAndSwap(0, int32(exitCode)) {
				cancelApp()
			}
		}
	} else if strings.TrimSpace(os.Getenv("AC_UPDATE_APPLY_MODE")) == httpapi.UpdateApplyManagedLauncherMode {
		logger.Warn("managed update mode ignored because launcher session authorization was not present")
	}
	if err := server.ValidateRuntimeDependencies(appCtx); err != nil {
		logger.Error("runtime dependency preflight failed", "error", err)
		os.Exit(1)
	}
	if server.StartMemoryWorkers(appCtx) {
		logger.Info("memory reprocessing worker enabled")
	}
	server.RegisterRoutes(mux)

	logger.Info("starting server", "bind", cfg.BindAddr, "mode", cfg.Mode, "log_level", parseLogLevel(os.Getenv("AC_LOG_LEVEL")).String())
	// 포크: 요청 로그 미들웨어로 감싼다. graceful shutdown은 upstream 구조 그대로.
	httpServer := &http.Server{Addr: cfg.BindAddr, Handler: withRequestLog(mux)}
	go func() {
		<-appCtx.Done()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful server shutdown failed", "error", err)
			_ = httpServer.Close()
		}
	}()
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
	if exitCode := requestedExitCode.Load(); exitCode != 0 {
		os.Exit(int(exitCode))
	}
}

const updateLauncherSessionContract = "archive-center.update-launcher-session.v1"

type updateLauncherSession struct {
	ContractVersion string `json:"contract_version"`
	Token           string `json:"token"`
}

func managedUpdateLauncherAuthorized(cfg config.Config) bool {
	if strings.TrimSpace(os.Getenv("AC_UPDATE_APPLY_MODE")) != httpapi.UpdateApplyManagedLauncherMode {
		return false
	}
	token := strings.TrimSpace(os.Getenv("AC_UPDATE_LAUNCHER_TOKEN"))
	if len(token) < 32 || strings.TrimSpace(cfg.UpdateStagingDir) == "" {
		return false
	}
	path := filepath.Join(cfg.UpdateStagingDir, "launcher-session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var session updateLauncherSession
	if json.Unmarshal(data, &session) != nil || session.ContractVersion != updateLauncherSessionContract || session.Token != token {
		return false
	}
	if err := os.Remove(path); err != nil {
		return false
	}
	_ = os.Unsetenv("AC_UPDATE_LAUNCHER_TOKEN")
	return true
}
