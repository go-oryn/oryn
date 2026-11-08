package health

import (
	"encoding/json"
	"net/http"
)

// NewHandler returns an [http.Handler] serving the checks on /livez, /readyz and /startupz.
func NewHandler(checker *Checker) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /livez", NewKindHandler(checker, Liveness))
	mux.Handle("GET /readyz", NewKindHandler(checker, Readiness))
	mux.Handle("GET /startupz", NewKindHandler(checker, Startup))

	return mux
}

// NewKindHandler returns an [http.Handler] running the check of the given kind.
// It answers 200 when healthy and 503 otherwise, with the probe results as JSON if the verbose query parameter is present.
func NewKindHandler(checker *Checker, kind Kind) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := checker.Check(r.Context(), kind)

		code := http.StatusOK
		if !result.Healthy() {
			code = http.StatusServiceUnavailable
		}

		if !r.URL.Query().Has("verbose") {
			w.WriteHeader(code)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)

		_ = json.NewEncoder(w).Encode(result)
	})
}
