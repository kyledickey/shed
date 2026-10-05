package store

import (
	"context"
	"fmt"
)

const projectCols = `id, name, created_at`

func scanProject(r scanner) (Project, error) {
	var p Project
	err := r.Scan(&p.ID, &p.Name, (*timestamp)(&p.CreatedAt))
	return p, err
}

// CreateProject creates a project. It returns ErrConflict if the name is
// taken.
func (s *Store) CreateProject(ctx context.Context, name string) (Project, error) {
	p := Project{ID: NewID(), Name: name, CreatedAt: now()}
	err := s.exec(ctx, `INSERT INTO projects (`+projectCols+`) VALUES (?, ?, ?)`,
		p.ID, p.Name, formatTime(p.CreatedAt))
	if err != nil {
		return Project{}, fmt.Errorf("store: create project %q: %w", name, err)
	}
	return p, nil
}

// Project returns the project with the given ID, or ErrNotFound.
func (s *Store) Project(ctx context.Context, id string) (Project, error) {
	p, err := queryOne(ctx, s, scanProject, `SELECT `+projectCols+` FROM projects WHERE id = ?`, id)
	if err != nil {
		return Project{}, fmt.Errorf("store: project %s: %w", id, err)
	}
	return p, nil
}

// Projects returns all projects, oldest first.
func (s *Store) Projects(ctx context.Context) ([]Project, error) {
	ps, err := queryAll(ctx, s, scanProject, `SELECT `+projectCols+` FROM projects ORDER BY created_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list projects: %w", err)
	}
	return ps, nil
}

// RenameProject changes a project's name. It returns ErrNotFound for an
// unknown project and ErrConflict if the name is taken.
func (s *Store) RenameProject(ctx context.Context, id, name string) error {
	if err := s.execOne(ctx, `UPDATE projects SET name = ? WHERE id = ?`, name, id); err != nil {
		return fmt.Errorf("store: rename project %s: %w", id, err)
	}
	return nil
}

// DeleteProject deletes a project and, by cascade, everything in it. It
// returns ErrNotFound for an unknown project.
func (s *Store) DeleteProject(ctx context.Context, id string) error {
	if err := s.execOne(ctx, `DELETE FROM projects WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete project %s: %w", id, err)
	}
	return nil
}
