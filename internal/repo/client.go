package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jesus/invoice-app/internal/model"
)

type ClientRepo struct {
	db *sql.DB
}

const clientCols = `id, name, email, phone, telegram_chat_id, pix_key, address, notes, created_at, updated_at`

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
		`INSERT INTO clients (`+clientCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Email, c.Phone, c.TelegramChatID, c.PIXKey, c.Address, c.Notes, c.CreatedAt, c.UpdatedAt,
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
		`UPDATE clients SET name = ?, email = ?, phone = ?, telegram_chat_id = ?, pix_key = ?, address = ?, notes = ?, updated_at = ? WHERE id = ?`,
		c.Name, c.Email, c.Phone, c.TelegramChatID, c.PIXKey, c.Address, c.Notes, c.UpdatedAt, c.ID)
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
	err := scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.TelegramChatID, &c.PIXKey, &c.Address, &c.Notes, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
