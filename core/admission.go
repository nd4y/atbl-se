package core

import (
	"errors"
	"net/http"
)

// Event streams and the single WebSocket have their own admission limits.
// Safety requests must still reach authorization/STOP when ordinary RPCs fill up.
func (s *Server) boundedHTTP(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		switch key {
		case "POST /api/stop", "POST /api/shutdown", "PUT /vacuglide/target-speed/stop", "PUT /vacuglide/sync-script/stop", "PUT /vacuglide/local-script/stop", "GET /events/stream", "GET /device-backend/ws":
			next(w, r)
			return
		}
		select {
		case s.requestSlots <- struct{}{}:
			defer func() { <-s.requestSlots }()
			next(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			if isCompatibilityPath(r.URL.Path) {
				compatibilityError(w, 429, "REQUEST_CAPACITY", "too many concurrent requests")
			} else {
				fail(w, 429, errors.New("too many concurrent requests"))
			}
		}
	}
}
