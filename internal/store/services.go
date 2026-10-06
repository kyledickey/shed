package store

import (
	"context"
	"fmt"
)

const serviceCols = `id, project_id, name, kind, repo, branch, root_dir, image, dockerfile_path,
	start_command, port, healthcheck_path, public_port, cpu_limit, memory_limit, auto_deploy, wait_for_ci, stopped, created_at`

func scanService(r scanner) (Service, error) {
	var v Service
	err := r.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Kind, &v.Repo, &v.Branch, &v.RootDir, &v.Image,
		&v.DockerfilePath, &v.StartCommand, &v.Port, &v.HealthcheckPath, &v.PublicPort,
		&v.CPULimit, &v.MemoryLimit, &v.AutoDeploy, &v.WaitForCI, &v.Stopped, (*timestamp)(&v.CreatedAt))
	return v, err
}

// CreateService stores sv under a new ID and creation time, which it returns
// along with the other fields. It returns ErrConflict if the project already
// has a service with that name.
func (s *Store) CreateService(ctx context.Context, sv Service) (Service, error) {
	sv.ID = NewID()
	sv.CreatedAt = now()
	err := s.exec(ctx, `INSERT INTO services (`+serviceCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sv.ID, sv.ProjectID, sv.Name, sv.Kind, sv.Repo, sv.Branch, sv.RootDir, sv.Image,
		sv.DockerfilePath, sv.StartCommand, sv.Port, sv.HealthcheckPath, sv.PublicPort,
		sv.CPULimit, sv.MemoryLimit, sv.AutoDeploy, sv.WaitForCI, sv.Stopped, formatTime(sv.CreatedAt))
	if err != nil {
		return Service{}, fmt.Errorf("store: create service %q: %w", sv.Name, err)
	}
	return sv, nil
}

// Service returns the service with the given ID, or ErrNotFound.
func (s *Store) Service(ctx context.Context, id string) (Service, error) {
	sv, err := queryOne(ctx, s, scanService, `SELECT `+serviceCols+` FROM services WHERE id = ?`, id)
	if err != nil {
		return Service{}, fmt.Errorf("store: service %s: %w", id, err)
	}
	return sv, nil
}

// Services returns the services of a project, oldest first.
func (s *Store) Services(ctx context.Context, projectID string) ([]Service, error) {
	svs, err := queryAll(ctx, s, scanService, `SELECT `+serviceCols+`
		FROM services WHERE project_id = ? ORDER BY created_at, rowid`, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: list services of project %s: %w", projectID, err)
	}
	return svs, nil
}

// AllServices returns every service, oldest first.
func (s *Store) AllServices(ctx context.Context) ([]Service, error) {
	svs, err := queryAll(ctx, s, scanService, `SELECT `+serviceCols+` FROM services ORDER BY created_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list services: %w", err)
	}
	return svs, nil
}

// ServicesForPush returns the app services that deploy automatically when the
// given branch of the given repository (owner/name, case-insensitive) is
// pushed.
func (s *Store) ServicesForPush(ctx context.Context, repo, branch string) ([]Service, error) {
	svs, err := queryAll(ctx, s, scanService, `SELECT `+serviceCols+`
		FROM services
		WHERE kind = 'app' AND auto_deploy = 1 AND branch = ? AND repo = ? COLLATE NOCASE
		ORDER BY created_at, rowid`, branch, repo)
	if err != nil {
		return nil, fmt.Errorf("store: list services for push to %s@%s: %w", repo, branch, err)
	}
	return svs, nil
}

// UpdateService overwrites the editable fields of the service sv.ID: its name,
// source, build, runtime, and resource settings. The ID, project, kind, and creation time
// are immutable, and Stopped is changed only by SetServiceStopped. It returns
// ErrNotFound for an unknown service.
func (s *Store) UpdateService(ctx context.Context, sv Service) error {
	err := s.execOne(ctx, `UPDATE services SET name = ?, repo = ?, branch = ?, root_dir = ?, image = ?,
		dockerfile_path = ?, start_command = ?, port = ?, healthcheck_path = ?, public_port = ?,
		cpu_limit = ?, memory_limit = ?, auto_deploy = ?, wait_for_ci = ? WHERE id = ?`,
		sv.Name, sv.Repo, sv.Branch, sv.RootDir, sv.Image, sv.DockerfilePath, sv.StartCommand,
		sv.Port, sv.HealthcheckPath, sv.PublicPort, sv.CPULimit, sv.MemoryLimit, sv.AutoDeploy,
		sv.WaitForCI, sv.ID)
	if err != nil {
		return fmt.Errorf("store: update service %s: %w", sv.ID, err)
	}
	return nil
}

// SetServiceStopped records whether a service has been stopped. It returns
// ErrNotFound for an unknown service.
func (s *Store) SetServiceStopped(ctx context.Context, id string, stopped bool) error {
	if err := s.execOne(ctx, `UPDATE services SET stopped = ? WHERE id = ?`, stopped, id); err != nil {
		return fmt.Errorf("store: set service %s stopped: %w", id, err)
	}
	return nil
}

// DeleteService deletes a service and, by cascade, its variables, volumes,
// domains, and deployments. It returns ErrNotFound for an unknown service.
func (s *Store) DeleteService(ctx context.Context, id string) error {
	if err := s.execOne(ctx, `DELETE FROM services WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete service %s: %w", id, err)
	}
	return nil
}

// Variables returns a service's variables.
func (s *Store) Variables(ctx context.Context, serviceID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM variables WHERE service_id = ?`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("store: variables of service %s: %w", serviceID, err)
	}
	defer rows.Close()
	vars := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("store: variables of service %s: %w", serviceID, err)
		}
		v, err := s.crypt.open(v, variableAD(serviceID, k))
		if err != nil {
			return nil, fmt.Errorf("store: variable %s of service %s: %w", k, serviceID, err)
		}
		vars[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: variables of service %s: %w", serviceID, err)
	}
	return vars, nil
}

// SetVariables atomically replaces all of a service's variables.
func (s *Store) SetVariables(ctx context.Context, serviceID string, vars map[string]string) error {
	if err := s.setVariables(ctx, serviceID, vars); err != nil {
		return fmt.Errorf("store: set variables of service %s: %w", serviceID, err)
	}
	return nil
}

func (s *Store) setVariables(ctx context.Context, serviceID string, vars map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM variables WHERE service_id = ?`, serviceID); err != nil {
		return err
	}
	for k, v := range vars {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO variables (service_id, key, value) VALUES (?, ?, ?)`,
			serviceID, k, s.crypt.seal(v, variableAD(serviceID, k))); err != nil {
			return err
		}
	}
	return tx.Commit()
}
