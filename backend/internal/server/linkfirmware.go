package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/brushknight/agents-deck/backend/internal/devices"
)

// linkFirmware: POST /local/link/firmware {"device", "board", "version",
// "file"} on the unix socket (`agentctl link update`): offers a board on
// Wi-Fi an app image from a file on this Mac. GET lists every board's
// firmware and its last update. Audited as "firmware <device>".
func (s *Server) linkFirmware(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if s.LinkFirmwareStatus == nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeJSON(w, http.StatusOK, s.LinkFirmwareStatus())
		return
	}
	var b struct {
		Device  string `json:"device"`
		Board   string `json:"board"`
		Version string `json:"version"`
		File    string `json:"file"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b); err != nil || b.Device == "" || b.Board == "" ||
		b.Version == "" || b.File == "" {
		writeErr(w, http.StatusBadRequest, `body must be {"device", "board", "version", "file"}`)
		return
	}
	if s.LinkFirmware == nil {
		writeErr(w, http.StatusServiceUnavailable, "Deck Link is off")
		return
	}
	code, msg := s.LinkFirmware(b.Device, b.Board, b.Version, b.File)
	if s.Audit != nil {
		s.Audit.Log(devices.AuditEntry{Device: "local", Action: "firmware " + b.Device, Status: code, Detail: b.Board + " " + b.Version})
	}
	if code >= 300 {
		writeErr(w, code, msg)
		return
	}
	writeJSON(w, code, map[string]string{"message": msg})
}
