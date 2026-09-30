package rest

import (
	"net/http"
	"strconv"
	"time"

	"github.com/iamroockie/plinth"
	"github.com/iamroockie/plinth/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func instrument(mux *http.ServeMux, reg prometheus.Registerer) middleware.Middleware {
	requests := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name: "pulsewatch_http_request_duration_seconds",
		Help: "Time spent handling an HTTP request.",
	}, []string{"route", "code"})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, route := mux.Handler(r)
			ww := plinth.NewResponseWriter(w)
			start := time.Now()

			next.ServeHTTP(ww, r)

			requests.WithLabelValues(route, strconv.Itoa(ww.Status())).
				Observe(time.Since(start).Seconds())
		})
	}
}
