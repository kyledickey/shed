package telemetry

import "go.opentelemetry.io/otel/attribute"

// Service names the service a record is about, in the attributes of the
// design's label schema.
type Service struct {
	ProjectID, ProjectName string
	ID, Name, Kind         string
}

// Labels returns s as metric data point attributes, named as
// VictoriaMetrics labels so they override those of shed's own resource.
func (s Service) Labels() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("shed_project_id", s.ProjectID),
		attribute.String("shed_project_name", s.ProjectName),
		attribute.String("shed_service_id", s.ID),
		attribute.String("shed_service_name", s.Name),
		attribute.String("shed_service_kind", s.Kind),
	}
}

// Attributes returns s as span attributes.
func (s Service) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("shed.project.id", s.ProjectID),
		attribute.String("shed.project.name", s.ProjectName),
		attribute.String("shed.service.id", s.ID),
		attribute.String("shed.service.name", s.Name),
		attribute.String("shed.service.kind", s.Kind),
	}
}
