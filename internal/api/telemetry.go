package api

import (
	"net/http"
	"time"

	"github.com/kyledickey/shed/internal/observability"
)

type telemetryComponentJSON struct {
	Name        observability.Component `json:"name"`
	State       observability.State     `json:"state"`
	Version     string                  `json:"version"`
	Error       string                  `json:"error"`
	CPUPercent  *float64                `json:"cpuPercent"`
	MemoryBytes *int64                  `json:"memoryBytes"`
	MemoryLimit int64                   `json:"memoryLimit"`
	DiskBytes   *int64                  `json:"diskBytes"`
}

type telemetryIngestJSON struct {
	Spans  float64 `json:"spans"`
	Logs   float64 `json:"logs"`
	Points float64 `json:"points"`
}

type telemetryDropJSON struct {
	Reason string  `json:"reason"`
	Signal string  `json:"signal"`
	Count  float64 `json:"count"`
}

type telemetryAlertJSON struct {
	Name      string    `json:"name"`
	ServiceID string    `json:"serviceId"`
	Since     time.Time `json:"since"`
}

type telemetryJSON struct {
	Settings        observability.Settings   `json:"settings"`
	Components      []telemetryComponentJSON `json:"components"`
	Ingest          telemetryIngestJSON      `json:"ingest"`
	DroppedLastHour []telemetryDropJSON      `json:"droppedLastHour"`
	FiringAlerts    []telemetryAlertJSON     `json:"firingAlerts"`
	GrafanaURL      string                   `json:"grafanaUrl"`
	OTLPEndpoint    string                   `json:"otlpEndpoint"`
}

func toTelemetryJSON(st observability.Status) telemetryJSON {
	out := telemetryJSON{
		Settings:        st.Settings,
		Components:      make([]telemetryComponentJSON, 0, len(st.Components)),
		Ingest:          telemetryIngestJSON{Spans: st.Ingest.Spans, Logs: st.Ingest.Logs, Points: st.Ingest.Points},
		DroppedLastHour: make([]telemetryDropJSON, 0, len(st.Dropped)),
		FiringAlerts:    make([]telemetryAlertJSON, 0, len(st.Alerts)),
		GrafanaURL:      st.GrafanaURL,
		OTLPEndpoint:    st.OTLPEndpoint,
	}
	for _, c := range st.Components {
		out.Components = append(out.Components, telemetryComponentJSON(c))
	}
	for _, d := range st.Dropped {
		out.DroppedLastHour = append(out.DroppedLastHour, telemetryDropJSON(d))
	}
	for _, a := range st.Alerts {
		out.FiringAlerts = append(out.FiringAlerts, telemetryAlertJSON(a))
	}
	return out
}

func (s *Server) getTelemetry(w http.ResponseWriter, r *http.Request) error {
	st, err := s.control.Telemetry(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toTelemetryJSON(st))
}

func (s *Server) putTelemetry(w http.ResponseWriter, r *http.Request) error {
	var in observability.Settings
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	st, err := s.control.SetTelemetry(r.Context(), in)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toTelemetryJSON(st))
}

func (s *Server) restartTelemetryComponent(w http.ResponseWriter, r *http.Request) error {
	st, err := s.control.RestartTelemetryComponent(r.Context(), r.PathValue("name"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toTelemetryJSON(st))
}

func (s *Server) deleteTelemetryData(w http.ResponseWriter, r *http.Request) error {
	if err := s.control.DeleteTelemetryData(r.Context()); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
