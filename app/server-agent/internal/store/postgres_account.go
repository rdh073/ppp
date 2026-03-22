package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// PostgresAccountStore persists platform accounts in PostgreSQL.
// All Save calls are upserts keyed by account ID.
type PostgresAccountStore struct {
	db *sql.DB
}

var _ AccountStore = (*PostgresAccountStore)(nil)

func NewPostgresAccountStore(db *sql.DB) *PostgresAccountStore {
	return &PostgresAccountStore{db: db}
}

func (s *PostgresAccountStore) Save(a domain.Account) error {
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO accounts (id, kind, device_id, persona_id, email, username, password, linked_account_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			kind              = EXCLUDED.kind,
			device_id         = EXCLUDED.device_id,
			persona_id        = EXCLUDED.persona_id,
			email             = EXCLUDED.email,
			username          = EXCLUDED.username,
			password          = EXCLUDED.password,
			linked_account_id = EXCLUDED.linked_account_id,
			status            = EXCLUDED.status
	`, a.ID, a.Kind, a.DeviceID, a.PersonaID, a.Email, a.Username, a.Password, a.LinkedAccountID, string(a.Status), a.CreatedAt)
	if err != nil {
		return fmt.Errorf("postgres account save: %w", err)
	}
	return nil
}

func (s *PostgresAccountStore) GetByID(id string) (domain.Account, bool) {
	row := s.db.QueryRowContext(context.Background(), `
		SELECT id, kind, device_id, persona_id, email, username, password, linked_account_id, status, created_at
		FROM accounts WHERE id = $1
	`, id)
	a, err := scanAccount(row)
	if err != nil {
		return domain.Account{}, false
	}
	return a, true
}

func (s *PostgresAccountStore) List(kind, deviceID string) []domain.Account {
	where, args := accountWhere(kind, deviceID)
	query := `SELECT id, kind, device_id, persona_id, email, username, password, linked_account_id, status, created_at
		FROM accounts` + where + ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []domain.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			continue
		}
		out = append(out, a)
	}
	return out
}

func (s *PostgresAccountStore) UpdateStatus(id string, status domain.AccountStatus) error {
	_, err := s.db.ExecContext(context.Background(),
		`UPDATE accounts SET status = $1 WHERE id = $2`, string(status), id)
	if err != nil {
		return fmt.Errorf("postgres account update status: %w", err)
	}
	return nil
}

func (s *PostgresAccountStore) UpdateDeviceID(id, deviceID string) error {
	_, err := s.db.ExecContext(context.Background(),
		`UPDATE accounts SET device_id = $1 WHERE id = $2`, deviceID, id)
	if err != nil {
		return fmt.Errorf("postgres account update device_id: %w", err)
	}
	return nil
}

func (s *PostgresAccountStore) FindActiveByKindOnDevice(deviceID, kind string) (domain.Account, bool) {
	row := s.db.QueryRowContext(context.Background(), `
		SELECT id, kind, device_id, persona_id, email, username, password, linked_account_id, status, created_at
		FROM accounts WHERE device_id = $1 AND kind = $2 AND status = 'active' LIMIT 1
	`, deviceID, kind)
	a, err := scanAccount(row)
	if err != nil {
		return domain.Account{}, false
	}
	return a, true
}

// --- helpers ---

func scanAccount(s scanner) (domain.Account, error) {
	var a domain.Account
	var status string
	err := s.Scan(&a.ID, &a.Kind, &a.DeviceID, &a.PersonaID, &a.Email, &a.Username, &a.Password, &a.LinkedAccountID, &status, &a.CreatedAt)
	if err != nil {
		return domain.Account{}, err
	}
	a.Status = domain.AccountStatus(status)
	return a, nil
}

func accountWhere(kind, deviceID string) (string, []any) {
	var conds []string
	var args []any
	n := 1
	if kind != "" {
		conds = append(conds, fmt.Sprintf("kind = $%d", n))
		args = append(args, kind)
		n++
	}
	if deviceID != "" {
		conds = append(conds, fmt.Sprintf("device_id = $%d", n))
		args = append(args, deviceID)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}
