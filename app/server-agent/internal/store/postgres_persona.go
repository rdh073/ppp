package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// PostgresPersonaStore persists personas in PostgreSQL.
// All Save calls are upserts keyed by persona ID.
type PostgresPersonaStore struct {
	db *sql.DB
}

var _ PersonaStore = (*PostgresPersonaStore)(nil)

func NewPostgresPersonaStore(db *sql.DB) *PostgresPersonaStore {
	return &PostgresPersonaStore{db: db}
}

func (s *PostgresPersonaStore) Save(p domain.Persona) error {
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO personas (id, kind, first_name, last_name, gender, birth_date, email, username, password, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE SET
			kind       = EXCLUDED.kind,
			first_name = EXCLUDED.first_name,
			last_name  = EXCLUDED.last_name,
			gender     = EXCLUDED.gender,
			birth_date = EXCLUDED.birth_date,
			email      = EXCLUDED.email,
			username   = EXCLUDED.username,
			password   = EXCLUDED.password,
			status     = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`, p.ID, p.Kind, p.FirstName, p.LastName, p.Gender, p.BirthDate, p.Email, p.Username, p.Password, string(p.Status), p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres persona save: %w", err)
	}
	return nil
}

func (s *PostgresPersonaStore) GetByID(id string) (domain.Persona, bool) {
	row := s.db.QueryRowContext(context.Background(), `
		SELECT id, kind, first_name, last_name, gender, birth_date, email, username, password, status, created_at, updated_at
		FROM personas WHERE id = $1
	`, id)
	p, err := scanPersona(row)
	if err != nil {
		return domain.Persona{}, false
	}
	return p, true
}

func (s *PostgresPersonaStore) List(kind, status string) []domain.Persona {
	where, args := personaWhere(kind, status)
	query := `SELECT id, kind, first_name, last_name, gender, birth_date, email, username, password, status, created_at, updated_at
		FROM personas` + where + ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []domain.Persona
	for rows.Next() {
		p, err := scanPersona(rows)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (s *PostgresPersonaStore) UpdateStatus(id string, status domain.PersonaStatus) error {
	_, err := s.db.ExecContext(context.Background(),
		`UPDATE personas SET status = $1, updated_at = $2 WHERE id = $3`,
		string(status), time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("postgres persona update status: %w", err)
	}
	return nil
}

func (s *PostgresPersonaStore) Delete(id string) (bool, error) {
	res, err := s.db.ExecContext(context.Background(), `DELETE FROM personas WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("postgres persona delete: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// --- helpers ---

func scanPersona(s scanner) (domain.Persona, error) {
	var p domain.Persona
	var status string
	err := s.Scan(&p.ID, &p.Kind, &p.FirstName, &p.LastName, &p.Gender, &p.BirthDate, &p.Email, &p.Username, &p.Password, &status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return domain.Persona{}, err
	}
	p.Status = domain.PersonaStatus(status)
	return p, nil
}

func personaWhere(kind, status string) (string, []any) {
	var conds []string
	var args []any
	n := 1
	if kind != "" {
		conds = append(conds, fmt.Sprintf("kind = $%d", n))
		args = append(args, kind)
		n++
	}
	if status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", n))
		args = append(args, status)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}
