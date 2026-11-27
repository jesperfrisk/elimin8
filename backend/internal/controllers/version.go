package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/jesperfrisk/elimin8/backend/internal/buildinfo"
)

// VersionController serves public, read-only build identification.
type VersionController struct {
	revision func() string
}

func NewVersionController() *VersionController {
	return &VersionController{revision: buildinfo.Revision}
}

// Version returns only the embedded build revision (or "unknown").
func (c *VersionController) Version(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Revision string `json:"revision"`
	}{Revision: c.revision()})
}
