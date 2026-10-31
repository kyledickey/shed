package grafana

import "testing"

func TestReconcile(t *testing.T) {
	rec := func(template, grafana int, customized bool) *Record {
		return &Record{TemplateVersion: template, GrafanaVersion: grafana, Customized: customized}
	}
	absent := Remote{}
	at := func(v int) Remote { return Remote{Exists: true, Version: v} }

	tests := []struct {
		name     string
		rec      *Record
		remote   Remote
		template int
		want     Action
	}{
		{"never created", nil, absent, 1, Create},
		{"deleted by the user", rec(1, 1, false), absent, 1, Leave},
		{"deleted by the user, template newer", rec(1, 1, false), absent, 2, Leave},
		{"deleted after customizing", rec(1, 3, true), absent, 2, Leave},
		{"present without a record", nil, at(4), 1, MarkCustomized},
		{"unchanged, template same", rec(2, 5, false), at(5), 2, Leave},
		{"unchanged, template newer", rec(1, 5, false), at(5), 2, Overwrite},
		{"unchanged, template older", rec(3, 5, false), at(5), 2, Leave},
		{"saved by a user", rec(1, 5, false), at(6), 1, MarkCustomized},
		{"saved by a user, template newer", rec(1, 5, false), at(6), 2, MarkCustomized},
		{"recreated with a lower version", rec(1, 5, false), at(1), 2, MarkCustomized},
		{"customized, template same", rec(1, 6, true), at(6), 1, Leave},
		{"customized, template newer", rec(1, 6, true), at(6), 2, Leave},
		{"customized, saved again", rec(1, 6, true), at(9), 2, Leave},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Reconcile(tt.rec, tt.remote, tt.template); got != tt.want {
				t.Errorf("Reconcile = %v, want %v", got, tt.want)
			}
		})
	}
}
