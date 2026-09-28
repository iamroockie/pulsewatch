package rest

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/iamroockie/plinth"
	"github.com/iamroockie/plinth/middleware"
)

func NewRouter(
	log *slog.Logger,
	probes map[string]plinth.CheckFunc,
	monitors MonitorService,
	history HistoryService,
) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /healthz", plinth.Healthz())
	mux.Handle("GET /readyz", plinth.Readyz(2*time.Second, probes))

	monitor := &monitorHandler{svc: monitors}
	mux.Handle("POST /monitors", plinth.RespondJSON(monitor.create))
	mux.Handle("GET /monitors", plinth.RespondJSON(monitor.list))
	mux.Handle("GET /monitors/{id}", plinth.RespondJSON(monitor.get))
	mux.Handle("PATCH /monitors/{id}", plinth.RespondJSON(monitor.update))
	mux.Handle("DELETE /monitors/{id}", plinth.RespondJSON(monitor.delete))

	hist := &historyHandler{svc: history}
	mux.Handle("GET /monitors/{id}/checks", plinth.RespondJSON(hist.checks))
	mux.Handle("GET /monitors/{id}/uptime", plinth.RespondJSON(hist.uptime))

	mw := middleware.Chain(
		middleware.RequestID(),
		middleware.RequestLog(log, "/healthz", "/readyz"),
		middleware.ErrorLog(log),
		middleware.Recover(),
	)

	return mw(plinth.JSONMux(mux))
}
