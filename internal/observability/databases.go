package observability

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// ServiceStore lists the database services and their deployments.
// *store.Store implements it.
type ServiceStore interface {
	AllServices(ctx context.Context) ([]store.Service, error)
	Project(ctx context.Context, id string) (store.Project, error)
	ActiveDeployment(ctx context.Context, serviceID string) (store.Deployment, error)
	Deployment(ctx context.Context, id string) (store.Deployment, error)
}

var _ ServiceStore = (*store.Store)(nil)

// Redeployer redeploys a deployment under a trigger. *deploy.Deployer
// implements it.
type Redeployer interface {
	RedeployAs(ctx context.Context, deploymentID string, trigger store.Trigger) (store.Deployment, error)
}

// exporterDocker is the part of Docker the exporters use.
type exporterDocker interface {
	List(ctx context.Context, labels map[string]string) ([]docker.Container, error)
	ResolveImage(ctx context.Context, ref string) (string, error)
	PullImage(ctx context.Context, ref string, w io.Writer) error
	Run(ctx context.Context, spec docker.RunSpec) (string, error)
	Start(ctx context.Context, id string) error
	Remove(ctx context.Context, id string) error
	Exec(ctx context.Context, id string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// databases keeps an exporter container next to every database with an
// active deployment, and tells VictoriaMetrics where to scrape them.
type databases struct {
	store      ServiceStore
	docker     exporterDocker
	redeployer Redeployer
	// publish replaces the scrape targets.
	publish func([]scrapeTarget) error
	log     *slog.Logger

	// every is how often the exporters are reconciled, and settle how often
	// a redeployment's progress is checked.
	every, settle time.Duration

	// prepared holds the deployments of postgres databases that
	// pg_stat_statements was created in. Only the worker touches it.
	prepared map[string]bool
	// failed holds the last error logged for each service, so a database
	// that stays broken is logged once.
	failed map[string]string
}

func newDatabases(cfg Config, publish func([]scrapeTarget) error) *databases {
	return &databases{
		store:      cfg.Databases,
		docker:     cfg.Docker,
		redeployer: cfg.Redeployer,
		publish:    publish,
		log:        cfg.Log,
		every:      10 * time.Second,
		settle:     2 * time.Second,
		prepared:   make(map[string]bool),
		failed:     make(map[string]string),
	}
}

// watch reconciles the exporters until ctx is done.
func (d *databases) watch(ctx context.Context) {
	for {
		if err := d.reconcile(ctx); err != nil && ctx.Err() == nil {
			d.logOnce("", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(d.every):
		}
	}
}

// logOnce logs err for key unless it was the last one logged for key.
func (d *databases) logOnce(key string, err error) {
	if err == nil {
		delete(d.failed, key)
		return
	}
	if d.failed[key] != err.Error() {
		d.failed[key] = err.Error()
		d.log.Warn("database telemetry", "service", key, "err", err)
	}
}

// active returns the databases that should have an exporter: services of a
// database kind that are not stopped and have an active deployment that
// recorded the variables it runs with.
func (d *databases) active(ctx context.Context) ([]database, error) {
	services, err := d.store.AllServices(ctx)
	if err != nil {
		return nil, err
	}
	var out []database
	projects := make(map[string]store.Project)
	for _, svc := range services {
		if _, ok := exportersByKind[svc.Kind]; !ok || svc.Stopped {
			continue
		}
		dep, err := d.store.ActiveDeployment(ctx, svc.ID)
		if errors.Is(err, store.ErrNotFound) || err == nil && (dep.ContainerID == "" || dep.Runtime == nil) {
			continue
		}
		if err != nil {
			return nil, err
		}
		p, ok := projects[svc.ProjectID]
		if !ok {
			if p, err = d.store.Project(ctx, svc.ProjectID); err != nil {
				return nil, err
			}
			projects[svc.ProjectID] = p
		}
		out = append(out, database{svc: svc, project: p, dep: dep})
	}
	slices.SortFunc(out, func(a, b database) int { return cmp.Compare(a.svc.ID, b.svc.ID) })
	return out, nil
}

// reconcile makes the exporter containers match the databases: it removes
// the ones whose database is gone or whose spec changed, creates the
// missing ones, publishes their addresses, and prepares the databases.
// A failure with one database does not hold up the others.
func (d *databases) reconcile(ctx context.Context) error {
	dbs, err := d.active(ctx)
	if err != nil {
		return err
	}
	have, err := d.docker.List(ctx, map[string]string{LabelExporter: ""})
	if err != nil {
		return err
	}
	byService := make(map[string]docker.Container, len(have))
	var errs []error
	for _, c := range have {
		id := c.Labels[LabelExporter]
		if !slices.ContainsFunc(dbs, func(db database) bool { return db.svc.ID == id }) {
			errs = append(errs, d.remove(ctx, c))
			continue
		}
		byService[id] = c
	}

	var targets []scrapeTarget
	for _, db := range dbs {
		c, err := d.ensure(ctx, db, byService[db.svc.ID])
		d.logOnce(db.svc.ID, err)
		if err != nil {
			continue
		}
		if ip := c.IPs[db.network()]; ip != "" {
			targets = append(targets, targetFor(db, ip))
		}
		d.logOnce(db.svc.ID+"/prepare", d.prepare(ctx, db))
	}
	errs = append(errs, d.publish(targets))
	return errors.Join(errs...)
}

// remove removes the exporter container c.
func (d *databases) remove(ctx context.Context, c docker.Container) error {
	d.log.Info("remove database exporter", "container", c.Name)
	return d.docker.Remove(ctx, c.ID)
}

// ensure returns the running exporter of db, creating it if cur is missing
// and replacing it if its spec changed.
func (d *databases) ensure(ctx context.Context, db database, cur docker.Container) (docker.Container, error) {
	want := exporterSpec(db)
	if cur.ID != "" && cur.Labels[labelExporterSpec] == want.Labels[labelExporterSpec] {
		if cur.Running || cur.State == "restarting" {
			return cur, nil
		}
		if err := d.docker.Start(ctx, cur.ID); err != nil {
			return cur, err
		}
		return d.find(ctx, db.svc.ID)
	}
	if cur.ID != "" {
		if err := d.remove(ctx, cur); err != nil {
			return cur, err
		}
	}
	if _, err := d.docker.ResolveImage(ctx, want.Image); err != nil {
		d.log.Info("pull exporter image", "image", want.Image)
		if err := d.docker.PullImage(ctx, want.Image, io.Discard); err != nil {
			return cur, err
		}
	}
	d.log.Info("start database exporter", "service", db.svc.Name, "kind", db.svc.Kind)
	if _, err := d.docker.Run(ctx, want); err != nil {
		return cur, err
	}
	return d.find(ctx, db.svc.ID)
}

// find returns the exporter container of a service.
func (d *databases) find(ctx context.Context, serviceID string) (docker.Container, error) {
	list, err := d.docker.List(ctx, map[string]string{LabelExporter: serviceID})
	if err != nil {
		return docker.Container{}, err
	}
	if len(list) == 0 {
		return docker.Container{}, fmt.Errorf("exporter of %s is gone", serviceID)
	}
	return list[0], nil
}

// createExtension creates pg_stat_statements in the database. The container
// has the variables it was started with, and the image trusts connections
// over its socket.
const createExtension = `export PGPASSWORD="$POSTGRES_PASSWORD"
user="${POSTGRES_USER:-postgres}"
exec psql -X -q -v ON_ERROR_STOP=1 -U "$user" -d "${POSTGRES_DB:-$user}" -c 'CREATE EXTENSION IF NOT EXISTS pg_stat_statements'`

// prepare creates pg_stat_statements once per deployment of a postgres
// database that was started with it preloaded. The extension lives in the
// database's data, so a restore can take it away; the next deployment puts
// it back.
func (d *databases) prepare(ctx context.Context, db database) error {
	if db.svc.Kind != "postgres" || !db.statements() || d.prepared[db.dep.ID] {
		return nil
	}
	var stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := d.docker.Exec(ctx, db.dep.ContainerID, []string{"sh", "-c", createExtension}, nil, io.Discard, &stderr)
	if err != nil {
		return fmt.Errorf("create pg_stat_statements: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	d.prepared[db.dep.ID] = true
	return nil
}

// removeAll removes every exporter container.
func (d *databases) removeAll(ctx context.Context) error {
	have, err := d.docker.List(ctx, map[string]string{LabelExporter: ""})
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range have {
		errs = append(errs, d.remove(ctx, c))
	}
	return errors.Join(errs...)
}

// redeployTimeout is how long one redeployment may take.
const redeployTimeout = 15 * time.Minute

// upgrade redeploys, one at a time, each database whose active deployment
// was started without the query statistics arguments, as a deployment with
// trigger telemetry. A database that is stopped, or runs a start command of
// its own, is left alone.
func (d *databases) upgrade(ctx context.Context) {
	if d.redeployer == nil {
		return
	}
	dbs, err := d.active(ctx)
	if err != nil {
		if ctx.Err() == nil {
			d.log.Warn("list databases to upgrade", "err", err)
		}
		return
	}
	for _, db := range dbs {
		if db.statements() || db.svc.StartCommand != "" {
			continue
		}
		if err := d.redeploy(ctx, db); err != nil {
			if ctx.Err() != nil {
				return
			}
			d.log.Warn("redeploy database for query statistics", "service", db.svc.Name, "err", err)
		}
	}
}

// redeploy redeploys the active deployment of db and waits until the new
// deployment ends.
func (d *databases) redeploy(ctx context.Context, db database) error {
	d.log.Info("redeploy database for query statistics", "service", db.svc.Name)
	dep, err := d.redeployer.RedeployAs(ctx, db.dep.ID, store.TriggerTelemetry)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, redeployTimeout)
	defer cancel()
	for {
		cur, err := d.store.Deployment(ctx, dep.ID)
		if err != nil {
			return err
		}
		if cur.Status.Terminal() {
			if cur.Status != store.StatusActive {
				return fmt.Errorf("deployment %s ended %s: %s", cur.ID, cur.Status, cur.Error)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d.settle):
		}
	}
}
