package server

import (
	"errors"
	"io"
	"net/http"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/auth"
	"github.com/ksuk/merlon/api/internal/domain"
	inboundwebhook "github.com/ksuk/merlon/api/internal/webhook"
)

func (s *Server) handleInboundCustomers(w http.ResponseWriter, r *http.Request) {
	s.handleInboundWebhook(w, r, inboundwebhook.KindCustomers)
}

func (s *Server) handleInboundTransactions(w http.ResponseWriter, r *http.Request) {
	s.handleInboundWebhook(w, r, inboundwebhook.KindTransactions)
}

func (s *Server) handleInboundWebhook(w http.ResponseWriter, r *http.Request, kind inboundwebhook.Kind) {
	if s.inboundWebhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "inbound webhooks not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, inboundwebhook.DefaultMaxBodyBytes+1))
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, "unable to read request body")
		return
	}
	event, err := s.inboundWebhooks.Accept(r.Context(), kind, r.Header, body)
	if err != nil {
		writeInboundWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, event)
}

func (s *Server) handleGetInboundEvent(w http.ResponseWriter, r *http.Request) {
	if s.inboundWebhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "inbound webhooks not configured")
		return
	}
	view, err := s.inboundWebhooks.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeInboundWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleReplayInboundEvent(w http.ResponseWriter, r *http.Request) {
	if s.inboundWebhooks == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "inbound webhooks not configured")
		return
	}
	if s.apikeys != nil {
		role, ok := auth.RoleFromContext(r.Context())
		if !ok || role != domain.RoleAdmin {
			writeErrorCode(w, http.StatusForbidden, apierr.CodeForbidden, "admin role required")
			return
		}
	}
	event, err := s.inboundWebhooks.Replay(r.Context(), r.PathValue("id"))
	if err != nil {
		writeInboundWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, event)
}

func writeInboundWebhookError(w http.ResponseWriter, err error) {
	var protocolErr *inboundwebhook.Error
	if errors.As(err, &protocolErr) {
		switch protocolErr.Code {
		case inboundwebhook.CodeUnauthorized, inboundwebhook.CodeInvalidSignature, inboundwebhook.CodeInvalidTimestamp:
			writeErrorCode(w, http.StatusUnauthorized, apierr.CodeUnauthorized, protocolErr.Error())
		case inboundwebhook.CodePayloadTooLarge:
			writeErrorCode(w, http.StatusRequestEntityTooLarge, apierr.CodePayloadTooLarge, protocolErr.Error())
		case inboundwebhook.CodeConflict:
			writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, protocolErr.Error())
		case inboundwebhook.CodeInvalidPayload:
			writeErrorCode(w, http.StatusBadRequest, apierr.CodeValidationFailed, protocolErr.Error())
		default:
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, protocolErr.Error())
		}
		return
	}
	if errors.Is(err, inboundwebhook.ErrNotFound) {
		writeErrorCode(w, http.StatusNotFound, apierr.CodeNotFound, err.Error())
		return
	}
	if errors.Is(err, inboundwebhook.ErrConflict) {
		writeErrorCode(w, http.StatusConflict, apierr.CodeConflict, err.Error())
		return
	}
	writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
}
