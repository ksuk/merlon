package server

import (
	"net/http"

	"github.com/ksuk/merlon/api/internal/requestid"
)

const requestIDHeader = "X-Request-ID"

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !requestid.Valid(id) {
			id = requestid.New()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(requestid.With(r.Context(), id)))
	})
}
