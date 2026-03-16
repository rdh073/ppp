package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/autosdk/ppp/account-service/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

// PostgresAccountStore persists account records in PostgreSQL.
// All Save methods are upserts keyed by account ID.
type PostgresAccountStore struct {
	db *sql.DB
}

func NewPostgresAccountStore(db *sql.DB) *PostgresAccountStore {
	return &PostgresAccountStore{db: db}
}

// --- Google ---

func (s *PostgresAccountStore) SaveGoogle(ctx context.Context, a *domain.GoogleAccount) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO google_accounts (id, email, password, device_id, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			email      = EXCLUDED.email,
			password   = EXCLUDED.password,
			device_id  = EXCLUDED.device_id,
			status     = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`, a.ID, a.Email, a.Password, a.DeviceID, string(a.Status), a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return mapPgError(err, "google")
	}
	return nil
}

func (s *PostgresAccountStore) GetGoogle(ctx context.Context, id domain.GoogleAccountID) (*domain.GoogleAccount, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, email, password, device_id, status, created_at, updated_at
		FROM google_accounts WHERE id = $1
	`, string(id))
	a, err := scanGoogle(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: google account %s", ErrNotFound, id)
	}
	return a, err
}

func (s *PostgresAccountStore) GetGoogleByEmail(ctx context.Context, email string) (*domain.GoogleAccount, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, email, password, device_id, status, created_at, updated_at
		FROM google_accounts WHERE email = $1
	`, email)
	a, err := scanGoogle(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: google account email %s", ErrNotFound, email)
	}
	return a, err
}

func (s *PostgresAccountStore) QueryGoogle(ctx context.Context, q GoogleAccountQuery) (GoogleAccountPage, error) {
	limit := clampLimit(q.Limit)
	where, args := googleWhere(q.DeviceID, string(q.Status))
	query := fmt.Sprintf(`
		SELECT id, email, password, device_id, status, created_at, updated_at,
		       COUNT(*) OVER() AS total
		FROM google_accounts
		%s
		ORDER BY updated_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)+1, len(args)+2)
	args = append(args, limit, q.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return GoogleAccountPage{}, fmt.Errorf("query google accounts: %w", err)
	}
	defer rows.Close()

	var items []*domain.GoogleAccount
	total := 0
	for rows.Next() {
		var a domain.GoogleAccount
		var status string
		if err := rows.Scan(
			&a.ID, &a.Email, &a.Password, &a.DeviceID, &status,
			&a.CreatedAt, &a.UpdatedAt, &total,
		); err != nil {
			return GoogleAccountPage{}, fmt.Errorf("scan google account: %w", err)
		}
		a.Status = domain.AccountStatus(status)
		items = append(items, &a)
	}
	if err := rows.Err(); err != nil {
		return GoogleAccountPage{}, fmt.Errorf("rows google accounts: %w", err)
	}
	return GoogleAccountPage{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  q.Offset,
		HasMore: q.Offset+len(items) < total,
	}, nil
}

// --- Instagram ---

func (s *PostgresAccountStore) SaveInstagram(ctx context.Context, a *domain.InstagramAccount) error {
	var googleID *string
	if a.GoogleAccountID != "" {
		v := string(a.GoogleAccountID)
		googleID = &v
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO instagram_accounts (id, username, contact, password, device_id, google_account_id, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			username          = EXCLUDED.username,
			contact           = EXCLUDED.contact,
			password          = EXCLUDED.password,
			device_id         = EXCLUDED.device_id,
			google_account_id = EXCLUDED.google_account_id,
			status            = EXCLUDED.status,
			updated_at        = EXCLUDED.updated_at
	`, a.ID, a.Username, a.Contact, a.Password, a.DeviceID, googleID, string(a.Status), a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return mapPgError(err, "instagram")
	}
	return nil
}

func (s *PostgresAccountStore) GetInstagram(ctx context.Context, id domain.InstagramAccountID) (*domain.InstagramAccount, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, contact, password, device_id, COALESCE(google_account_id,''), status, created_at, updated_at
		FROM instagram_accounts WHERE id = $1
	`, string(id))
	a, err := scanInstagram(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: instagram account %s", ErrNotFound, id)
	}
	return a, err
}

func (s *PostgresAccountStore) GetInstagramByUsername(ctx context.Context, username string) (*domain.InstagramAccount, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, contact, password, device_id, COALESCE(google_account_id,''), status, created_at, updated_at
		FROM instagram_accounts WHERE username = $1
	`, username)
	a, err := scanInstagram(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: instagram account username %s", ErrNotFound, username)
	}
	return a, err
}

func (s *PostgresAccountStore) QueryInstagram(ctx context.Context, q InstagramAccountQuery) (InstagramAccountPage, error) {
	limit := clampLimit(q.Limit)
	where, args := instagramWhere(q.DeviceID, string(q.Status), string(q.GoogleAccountID))
	query := fmt.Sprintf(`
		SELECT id, username, contact, password, device_id, COALESCE(google_account_id,''), status, created_at, updated_at,
		       COUNT(*) OVER() AS total
		FROM instagram_accounts
		%s
		ORDER BY updated_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)+1, len(args)+2)
	args = append(args, limit, q.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return InstagramAccountPage{}, fmt.Errorf("query instagram accounts: %w", err)
	}
	defer rows.Close()

	var items []*domain.InstagramAccount
	total := 0
	for rows.Next() {
		var a domain.InstagramAccount
		var status, googleID string
		if err := rows.Scan(
			&a.ID, &a.Username, &a.Contact, &a.Password, &a.DeviceID,
			&googleID, &status, &a.CreatedAt, &a.UpdatedAt, &total,
		); err != nil {
			return InstagramAccountPage{}, fmt.Errorf("scan instagram account: %w", err)
		}
		a.Status = domain.AccountStatus(status)
		a.GoogleAccountID = domain.GoogleAccountID(googleID)
		items = append(items, &a)
	}
	if err := rows.Err(); err != nil {
		return InstagramAccountPage{}, fmt.Errorf("rows instagram accounts: %w", err)
	}
	return InstagramAccountPage{
		Items:   items,
		Total:   total,
		Limit:   limit,
		Offset:  q.Offset,
		HasMore: q.Offset+len(items) < total,
	}, nil
}

// --- helpers ---

type scanner interface {
	Scan(dest ...any) error
}

func scanGoogle(row scanner) (*domain.GoogleAccount, error) {
	var a domain.GoogleAccount
	var status string
	if err := row.Scan(&a.ID, &a.Email, &a.Password, &a.DeviceID, &status, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.Status = domain.AccountStatus(status)
	return &a, nil
}

func scanInstagram(row scanner) (*domain.InstagramAccount, error) {
	var a domain.InstagramAccount
	var status, googleID string
	if err := row.Scan(&a.ID, &a.Username, &a.Contact, &a.Password, &a.DeviceID, &googleID, &status, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.Status = domain.AccountStatus(status)
	a.GoogleAccountID = domain.GoogleAccountID(googleID)
	return &a, nil
}

func googleWhere(deviceID, status string) (string, []any) {
	var clauses []string
	var args []any
	if deviceID != "" {
		args = append(args, deviceID)
		clauses = append(clauses, fmt.Sprintf("device_id = $%d", len(args)))
	}
	if status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func instagramWhere(deviceID, status, googleAccountID string) (string, []any) {
	var clauses []string
	var args []any
	if deviceID != "" {
		args = append(args, deviceID)
		clauses = append(clauses, fmt.Sprintf("device_id = $%d", len(args)))
	}
	if status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if googleAccountID != "" {
		args = append(args, googleAccountID)
		clauses = append(clauses, fmt.Sprintf("google_account_id = $%d", len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

// mapPgError translates PostgreSQL unique-violation errors into domain errors.
func mapPgError(err error, kind string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		switch {
		case strings.Contains(pgErr.ConstraintName, "email"):
			return fmt.Errorf("%w: %s", ErrDuplicateEmail, pgErr.Detail)
		case strings.Contains(pgErr.ConstraintName, "username"):
			return fmt.Errorf("%w: %s", ErrDuplicateUsername, pgErr.Detail)
		}
	}
	return fmt.Errorf("postgres %s: %w", kind, err)
}
