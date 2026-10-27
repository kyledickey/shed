package grafana

// Record is what shed remembers about a dashboard it created.
type Record struct {
	// TemplateVersion is the template version shed last wrote.
	TemplateVersion int
	// GrafanaVersion is Grafana's dashboard version after that write.
	GrafanaVersion int
	// Customized is set once a user has saved the dashboard.
	Customized bool
}

// Remote is the state of a dashboard in Grafana.
type Remote struct {
	Exists  bool
	Version int
}

// Action is what reconciliation does with one dashboard.
type Action int

const (
	// Leave does nothing.
	Leave Action = iota
	// Create writes the dashboard and records the template and new Grafana
	// versions.
	Create
	// Overwrite replaces the dashboard with the current template and
	// records the new versions.
	Overwrite
	// MarkCustomized records the dashboard as customized at Grafana's
	// version without writing it.
	MarkCustomized
)

func (a Action) String() string {
	switch a {
	case Create:
		return "create"
	case Overwrite:
		return "overwrite"
	case MarkCustomized:
		return "mark customized"
	}
	return "leave"
}

// Reconcile decides what to do with a dashboard, given shed's record of it
// (nil if shed never created it), its state in Grafana, and the current
// version of its template.
func Reconcile(rec *Record, remote Remote, template int) Action {
	switch {
	case !remote.Exists && rec == nil:
		return Create
	case !remote.Exists:
		// The user deleted it; "Restore default dashboards" brings it back.
		return Leave
	case rec == nil:
		// It exists under shed's UID but shed lost its record, as after
		// restoring an older shed.db; keep whatever is there.
		return MarkCustomized
	case rec.Customized:
		return Leave
	case remote.Version != rec.GrafanaVersion:
		// Saved since shed's write, or deleted and recreated.
		return MarkCustomized
	case template > rec.TemplateVersion:
		return Overwrite
	}
	return Leave
}
