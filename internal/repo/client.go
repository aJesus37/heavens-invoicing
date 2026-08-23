package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jesus/invoice-app/internal/model"
)

type ClientRepo struct {
	db *sql.DB
}

const clientCols = `id, name, email, phone, telegram_chat_id, pix_key, address, notes, language, created_at, updated_at`

func (r *ClientRepo) Create(ctx context.Context, c *model.Client) (*model.Client, error) {
	if c.ID == "" {
		c.ID = model.NewID()
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = now
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO clients (`+clientCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Email, c.Phone, c.TelegramChatID, c.PIXKey, c.Address, c.Notes, c.Language, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *ClientRepo) Get(ctx context.Context, id string) (*model.Client, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+clientCols+` FROM clients WHERE id = ?`, id)
	return scanClient(row.Scan)
}

func (r *ClientRepo) Update(ctx context.Context, c *model.Client) error {
	c.UpdatedAt = time.Now().UTC()
	res, err := r.db.ExecContext(ctx,
		`UPDATE clients SET name = ?, email = ?, phone = ?, telegram_chat_id = ?, pix_key = ?, address = ?, notes = ?, language = ?, updated_at = ? WHERE id = ?`,
		c.Name, c.Email, c.Phone, c.TelegramChatID, c.PIXKey, c.Address, c.Notes, c.Language, c.UpdatedAt, c.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update client: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

const ClientPageSize = 20

// ListPaginated returns a page of clients filtered by free-text q (LIKE on
// name/email/phone). Pagination is LIMIT 20 OFFSET (page-1)*20 ordered by name.
func (r *ClientRepo) ListPaginated(ctx context.Context, page int, q string) ([]*model.Client, int, error) {
	if page < 1 {
		page = 1
	}
	q = strings.TrimSpace(q)
	var where string
	var args []any
	if q != "" {
		like := "%" + q + "%"
		where = "WHERE name LIKE ? OR email LIKE ? OR phone LIKE ?"
		args = append(args, like, like, like)
	}
	var total int
	countQuery := `SELECT COUNT(*) FROM clients ` + where
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count clients: %w", err)
	}
	limit := ClientPageSize
	offset := (page - 1) * limit
	dataArgs := append(append([]any{}, args...), limit, offset)
	dataQuery := `SELECT ` + clientCols + ` FROM clients ` + where + ` ORDER BY name LIMIT ? OFFSET ?`
	rows, err := r.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list clients paginated: %w", err)
	}
	defer rows.Close()
	clients := make([]*model.Client, 0)
	for rows.Next() {
		c, err := scanClient(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		clients = append(clients, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return clients, total, nil
}

func (r *ClientRepo) List(ctx context.Context) ([]*model.Client, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+clientCols+` FROM clients ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	clients := make([]*model.Client, 0)
	for rows.Next() {
		c, err := scanClient(rows.Scan)
		if err != nil {
			return nil, err
		}
		clients = append(clients, c)
	}
	return clients, rows.Err()
}

func (r *ClientRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM clients WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete client: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanClient(scan func(dest ...any) error) (*model.Client, error) {
	var c model.Client
	err := scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.TelegramChatID, &c.PIXKey, &c.Address, &c.Notes, &c.Language, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
