package models

// HealthResponse contains public infrastructure status, without connection details.
type HealthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database,omitempty"`
}
