package telemetry

import "time"

// Event is a moment, or a stretch of time, in a service's life that
// dashboards mark: a deployment, restart, stop, crash, backup, or restore.
type Event struct {
	Kind EventKind
	// ID is the deployment, backup, or restore ID; empty if none.
	ID string
	// Service is the service the event is about; a zero ID means shed
	// itself, as for backups of shed.db.
	Service Service
	Start   time.Time
	// End is zero for a moment.
	End  time.Time
	Text string
	// OOM is set on a crash when the kernel killed the service for running
	// out of memory.
	OOM bool
}

// EventKind names what happened.
type EventKind string

// The kinds of events.
const (
	EventDeployStarted EventKind = "deploy.started"
	EventRedeploy      EventKind = "redeploy" // a redeploy started
	EventDeployActive  EventKind = "deploy.active"
	EventDeployFailed  EventKind = "deploy.failed"
	EventRestart       EventKind = "restart"
	EventStop          EventKind = "stop"
	EventCrash         EventKind = "crash"
	EventBackup        EventKind = "backup"
	EventBackupFailed  EventKind = "backup.failed"
	EventRestore       EventKind = "restore"
	EventRestoreFailed EventKind = "restore.failed"
)
