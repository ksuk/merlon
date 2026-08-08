package server

import (
	"errors"
	"net/http"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

func writeWave3Error(w http.ResponseWriter, err error) {
	var notFound *domain.ErrNotFound
	var conflict *domain.ErrConflict
	switch {
	case errors.As(err, &notFound):
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
	case errors.As(err, &conflict):
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
	default:
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
	}
}
