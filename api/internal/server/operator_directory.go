package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/ksuk/merlon/api/internal/apierr"
	"github.com/ksuk/merlon/api/internal/domain"
)

type operatorDirectoryUser struct {
	ID    string      `json:"id"`
	Email string      `json:"email"`
	Role  domain.Role `json:"role"`
}

type operatorDirectoryResponse struct {
	Users []operatorDirectoryUser `json:"users"`
	Teams []string                `json:"teams"`
}

type operatorAssignmentValidationError struct{ reason string }

func (e *operatorAssignmentValidationError) Error() string { return e.reason }

func (s *Server) knownAssignmentTeams(_ *http.Request) ([]string, error) {
	teamSet := make(map[string]struct{})
	for _, configured := range s.operatorTeams {
		if team := strings.TrimSpace(configured); team != "" {
			teamSet[team] = struct{}{}
		}
	}
	teams := make([]string, 0, len(teamSet))
	for team := range teamSet {
		teams = append(teams, team)
	}
	sort.Strings(teams)
	return teams, nil
}

func (s *Server) validateKnownTeam(r *http.Request, teamID string) error {
	teamID = strings.TrimSpace(teamID)
	if teamID == "" || s.apikeys == nil {
		return nil
	}
	teams, err := s.knownAssignmentTeams(r)
	if err != nil {
		return err
	}
	// The configured directory is authoritative. An empty directory in an
	// authenticated deployment is fail-closed; accepting an arbitrary value
	// would recreate the queue-derived directory bug.
	if len(teams) == 0 {
		return &operatorAssignmentValidationError{reason: "operator team directory is not configured"}
	}
	for _, team := range teams {
		if team == teamID {
			return nil
		}
	}
	return &operatorAssignmentValidationError{reason: "assigned_team must reference a known team"}
}

// validateKnownOperator keeps authenticated deployments from accepting an
// arbitrary operator identifier that cannot be resolved to an active user.
// Database-free development mode intentionally keeps the legacy free-text
// behavior because it has no configured identity directory.
func (s *Server) validateKnownOperator(r *http.Request, operatorID string) error {
	operatorID = strings.TrimSpace(operatorID)
	if operatorID == "" || s.users == nil || s.apikeys == nil {
		return nil
	}
	users, err := s.users.List(r.Context())
	if err != nil {
		return fmt.Errorf("operator directory lookup failed: %w", err)
	}
	for _, user := range users {
		if user.Active && user.ID == operatorID {
			return nil
		}
	}
	return &operatorAssignmentValidationError{reason: "assigned_to must reference an active operator"}
}

// handleListOperatorDirectory exposes only active assignment candidates. It
// is deliberately not the admin user-management endpoint and never includes
// password hashes or inactive users.
func (s *Server) handleListOperatorDirectory(w http.ResponseWriter, r *http.Request) {
	response := operatorDirectoryResponse{Users: []operatorDirectoryUser{}, Teams: []string{}}
	if s.users != nil {
		users, err := s.users.List(r.Context())
		if err != nil {
			writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
			return
		}
		for _, user := range users {
			if !user.Active {
				continue
			}
			response.Users = append(response.Users, operatorDirectoryUser{ID: user.ID, Email: user.Email, Role: user.Role})
		}
		sort.Slice(response.Users, func(i, j int) bool { return response.Users[i].ID < response.Users[j].ID })
	}

	teams, err := s.knownAssignmentTeams(r)
	if err != nil {
		writeErrorCode(w, http.StatusInternalServerError, apierr.CodeInternal, err.Error())
		return
	}
	response.Teams = teams
	writeJSON(w, http.StatusOK, response)
}
