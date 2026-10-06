package control

import (
	"errors"
	"fmt"

	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/update"
)

// The kinds of failure an [*Error] reports.
var (
	// ErrInvalid reports a request that breaks a rule, such as a service
	// name that is not a DNS label.
	ErrInvalid = errors.New("control: invalid")
	// ErrNotFound reports that a project, service, deployment, domain,
	// volume, or backup does not exist. It is store.ErrNotFound, so the
	// store's lookups match it as they are.
	ErrNotFound = store.ErrNotFound
	// ErrConflict reports a request that the current state does not allow,
	// such as a name already in use or a service that is busy.
	ErrConflict = errors.New("control: conflict")
	// ErrUpstream reports that GitHub failed a request.
	ErrUpstream = errors.New("control: upstream")
	// ErrUnavailable reports that shed is shutting down.
	ErrUnavailable = errors.New("control: unavailable")
)

// Error is a failure with a message fit to show to the user. Kind is one of
// ErrInvalid, ErrNotFound, ErrConflict, ErrUpstream, or ErrUnavailable, and
// errors.Is matches it.
type Error struct {
	Kind error
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// Unwrap returns e.Kind.
func (e *Error) Unwrap() error { return e.Kind }

func errorf(kind error, format string, args ...any) error {
	return &Error{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// msgFenced explains why a fenced service cannot be started, deployed, or
// restored into.
const msgFenced = "a restore of this service failed and its data may be incomplete; " +
	"restart shed to retry recovering it, or clear the restore fence to keep the data as it is"

// known maps the sentinel errors of the packages below Plane to a kind and a
// message. An empty message means the error's own text, which those errors
// write for the user.
var known = []struct {
	err  error
	kind error
	msg  string
}{
	{store.ErrNotFound, ErrNotFound, "not found"},
	{store.ErrConflict, ErrConflict, "already exists"},
	{deploy.ErrNotInProgress, ErrConflict, "deployment is not in progress"},
	{deploy.ErrVolumeMissing, ErrConflict, "a volume of this service is missing on the server; restore it from a backup, or deploy again to start with an empty volume"},
	{deploy.ErrImageUnavailable, ErrConflict, "the deployment's image is no longer on the server; deploy again to build or pull it"},
	{deploy.ErrNoImage, ErrConflict, "deployment has no image to redeploy"},
	{deploy.ErrNoContainer, ErrConflict, "nothing to run; deploy the service first"},
	{deploy.ErrServiceStopped, ErrConflict, "service is stopped; start it instead"},
	{deploy.ErrServiceBusy, ErrConflict, "service is busy with a backup or restore; try again when it finishes"},
	{deploy.ErrFenced, ErrConflict, msgFenced},
	{backup.ErrFenced, ErrConflict, msgFenced},
	{deploy.ErrDeleting, ErrConflict, "service is being deleted"},
	{deploy.ErrStopped, ErrUnavailable, "shutting down"},
	{backup.ErrStopped, ErrUnavailable, "shutting down"},
	{backup.ErrInvalid, ErrInvalid, ""},
	{backup.ErrNoVolumes, ErrInvalid, "service has no volumes to back up"},
	{backup.ErrBusy, ErrConflict, "a backup or restore is already in progress"},
	{update.ErrUnsupported, ErrConflict, "this build of shed cannot update itself"},
	{update.ErrNoUpdate, ErrConflict, "there is no newer release"},
	{update.ErrBusy, ErrConflict, "an update check, download, or install is already in progress"},
	{update.ErrNotStaged, ErrConflict, "download the update before installing it"},
}

// Explain returns err as an *Error: itself if it is one, or the kind and
// message of a known condition, such as a missing record or a busy service.
// It returns false for unexpected errors, whose text is not meant for users.
func Explain(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	for _, k := range known {
		if errors.Is(err, k.err) {
			msg := k.msg
			if msg == "" {
				msg = err.Error()
			}
			return &Error{Kind: k.kind, Msg: msg}, true
		}
	}
	return nil, false
}
