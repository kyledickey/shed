package api

import (
	"net/http"
	"time"

	"github.com/kyledickey/shed/internal/control"
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

func (s *Server) resetDashboard(w http.ResponseWriter, r *http.Request) error {
	if err := s.control.ResetDashboard(r.Context(), r.PathValue("uid")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) restoreDashboards(w http.ResponseWriter, r *http.Request) error {
	if err := s.control.RestoreDashboards(r.Context()); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type dashboardLinkJSON struct {
	UID        string `json:"uid"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Customized bool   `json:"customized"`
	Outdated   bool   `json:"outdated"`
}

type serviceAlertJSON struct {
	Name  string    `json:"name"`
	Since time.Time `json:"since"`
}

type serviceTelemetryJSON struct {
	Caps          observability.Caps  `json:"caps"`
	Overridden    bool                `json:"overridden"`
	Dropping      bool                `json:"dropping"`
	ListenerError string              `json:"listenerError"`
	FolderURL     string              `json:"folderUrl"`
	Dashboards    []dashboardLinkJSON `json:"dashboards"`
	FiringAlerts  []serviceAlertJSON  `json:"firingAlerts"`
}

func toServiceTelemetryJSON(t control.ServiceTelemetry) serviceTelemetryJSON {
	out := serviceTelemetryJSON{
		Caps:          t.Caps,
		Overridden:    t.Overridden,
		Dropping:      t.Dropping,
		ListenerError: t.ListenerError,
		FolderURL:     t.Grafana.FolderURL,
		Dashboards:    make([]dashboardLinkJSON, 0, len(t.Grafana.Dashboards)),
		FiringAlerts:  make([]serviceAlertJSON, 0, len(t.Grafana.Alerts)),
	}
	for _, d := range t.Grafana.Dashboards {
		out.Dashboards = append(out.Dashboards, dashboardLinkJSON(d))
	}
	for _, a := range t.Grafana.Alerts {
		out.FiringAlerts = append(out.FiringAlerts, serviceAlertJSON{Name: a.Name, Since: a.Since})
	}
	return out
}

func (s *Server) getServiceTelemetry(w http.ResponseWriter, r *http.Request) error {
	st, err := s.control.ServiceTelemetry(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toServiceTelemetryJSON(st))
}

func (s *Server) putServiceTelemetry(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Caps *observability.Caps `json:"caps"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	st, err := s.control.SetServiceTelemetryCaps(r.Context(), r.PathValue("id"), in.Caps)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toServiceTelemetryJSON(st))
}
