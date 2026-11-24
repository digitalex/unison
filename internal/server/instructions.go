package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/digitalex/unison/internal/store"
)

func (s *Server) getWorkspaceInstructions(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetWorkspaceInstructions(r.Context(), r.PathValue("ws"))
	if err != nil {
		storeError(w, err)
		return
	}
	if section := strings.TrimSpace(r.URL.Query().Get("section")); section != "" {
		matched, body, ok := store.ExtractInstructionSection(inst.Instructions, section)
		if !ok {
			writeError(w, http.StatusNotFound, fmt.Sprintf("section %q not found", section))
			return
		}
		inst.Instructions = body
		inst.Section = matched
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) updateWorkspaceInstructions(w http.ResponseWriter, r *http.Request) {
	var in store.InstructionsInput
	if !decode(w, r, &in) {
		return
	}
	inst, evs, err := s.store.SetWorkspaceInstructions(r.Context(), r.PathValue("ws"), actor(r), in)
	s.respond(w, http.StatusOK, inst, evs, err)
}
