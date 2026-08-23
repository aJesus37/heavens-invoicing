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

type ProductRepo struct {
	db *sql.DB
}

const productCols = `id, name, description, unit_price, currency, active, created_at, updated_at`

func (r *ProductRepo) Create(ctx context.Context, p *model.Product) (*model.Product, error) {
	if p.ID == "" {
		p.ID = model.NewID()
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	if p.Currency == "" {
		p.Currency = "BRL"
	}
	// Caller-provided Currency is honored (empty -> BRL). New products always
	// start active (DB default 1); call Update to deactivate.
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO products (id, name, description, unit_price, currency, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Description, p.UnitPrice, p.Currency, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	p.Active = true
	return p, nil
}

func (r *ProductRepo) Get(ctx context.Context, id string) (*model.Product, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+productCols+` FROM products WHERE id = ?`, id)
	return scanProduct(row.Scan)
}

func (r *ProductRepo) Update(ctx context.Context, p *model.Product) error {
	p.UpdatedAt = time.Now().UTC()
	active := 0
	if p.Active {
		active = 1
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE products SET name = ?, description = ?, unit_price = ?, currency = ?, active = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.Description, p.UnitPrice, p.Currency, active, p.UpdatedAt, p.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update product: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

const ProductPageSize = 20

// ListPaginated returns a page of products filtered by free-text q (LIKE on
// name/description). Pagination is LIMIT 20 OFFSET (page-1)*20 ordered by name.
func (r *ProductRepo) ListPaginated(ctx context.Context, page int, q string) ([]*model.Product, int, error) {
	if page < 1 {
		page = 1
	}
	q = strings.TrimSpace(q)
	var where string
	var args []any
	if q != "" {
		like := "%" + q + "%"
		where = "WHERE name LIKE ? OR description LIKE ?"
		args = append(args, like, like)
	}
	var total int
	countQuery := `SELECT COUNT(*) FROM products ` + where
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}
	limit := ProductPageSize
	offset := (page - 1) * limit
	dataArgs := append(append([]any{}, args...), limit, offset)
	dataQuery := `SELECT ` + productCols + ` FROM products ` + where + ` ORDER BY name LIMIT ? OFFSET ?`
	rows, err := r.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list products paginated: %w", err)
	}
	defer rows.Close()
	products := make([]*model.Product, 0)
	for rows.Next() {
		p, err := scanProduct(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return products, total, nil
}

func (r *ProductRepo) List(ctx context.Context) ([]*model.Product, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+productCols+` FROM products ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	products := make([]*model.Product, 0)
	for rows.Next() {
		p, err := scanProduct(rows.Scan)
		if err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

func (r *ProductRepo) ListActive(ctx context.Context) ([]*model.Product, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+productCols+` FROM products WHERE active = 1 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	products := make([]*model.Product, 0)
	for rows.Next() {
		p, err := scanProduct(rows.Scan)
		if err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

func (r *ProductRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM products WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete product: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanProduct(scan func(dest ...any) error) (*model.Product, error) {
	var (
		p      model.Product
		active int
	)
	err := scan(&p.ID, &p.Name, &p.Description, &p.UnitPrice, &p.Currency, &active, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Active = active == 1
	return &p, nil
}
