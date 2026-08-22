package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jesus/invoice-app/internal/model"
)

type InvoiceRepo struct {
	db *sql.DB
}

const invoiceCols = `id, client_id, number, status, issue_date, due_date, subtotal, total, notes, pix_key, pdf_path, created_at, updated_at`
const invoiceItemCols = `id, invoice_id, product_id, description, unit_price, quantity, total`

var invoiceStatuses = []string{"draft", "sent", "paid", "overdue", "cancelled"}

func validInvoiceStatus(s string) bool { return slices.Contains(invoiceStatuses, s) }

func (r *InvoiceRepo) Create(ctx context.Context, inv *model.Invoice) error {
	if len(inv.Items) == 0 {
		return fmt.Errorf("create invoice: at least one item is required")
	}
	var subtotal int64
	for _, it := range inv.Items {
		if it.Description == "" {
			return fmt.Errorf("create invoice: every item needs a description")
		}
		if it.Quantity < 1 {
			return fmt.Errorf("create invoice: item %q quantity must be >= 1", it.Description)
		}
		it.Total = it.UnitPrice * it.Quantity
		subtotal += it.Total
	}
	if inv.Status == "" {
		inv.Status = "draft"
	}
	if !validInvoiceStatus(inv.Status) {
		return fmt.Errorf("create invoice: invalid status %q", inv.Status)
	}

	if inv.ID == "" {
		inv.ID = model.NewID()
	}
	now := time.Now().UTC()
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = now
	}
	if inv.UpdatedAt.IsZero() {
		inv.UpdatedAt = now
	}
	inv.Subtotal = subtotal
	inv.Total = subtotal

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("create invoice: %w", err)
	}
	defer tx.Rollback()

	var number int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(number),0)+1 FROM invoices`).Scan(&number); err != nil {
		return fmt.Errorf("create invoice: next number: %w", err)
	}
	inv.Number = number

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO invoices (`+invoiceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.ID, inv.ClientID, inv.Number, inv.Status, inv.IssueDate, inv.DueDate, inv.Subtotal, inv.Total, inv.Notes, inv.PIXKey, inv.PDFPath, inv.CreatedAt, inv.UpdatedAt,
	); err != nil {
		return fmt.Errorf("create invoice: %w", err)
	}
	for _, it := range inv.Items {
		if it.ID == "" {
			it.ID = model.NewID()
		}
		it.InvoiceID = inv.ID
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO invoice_items (`+invoiceItemCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			it.ID, it.InvoiceID, it.ProductID, it.Description, it.UnitPrice, it.Quantity, it.Total,
		); err != nil {
			return fmt.Errorf("create invoice: insert item %q: %w", it.Description, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create invoice: %w", err)
	}
	return nil
}

func (r *InvoiceRepo) Get(ctx context.Context, id string) (*model.Invoice, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+invoiceCols+` FROM invoices WHERE id = ?`, id)
	inv, err := scanInvoice(row.Scan)
	if err != nil {
		return nil, err
	}
	items, err := r.itemsFor(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get invoice: %w", err)
	}
	inv.Items = items
	return inv, nil
}

func (r *InvoiceRepo) GetByNumber(ctx context.Context, number int64) (*model.Invoice, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+invoiceCols+` FROM invoices WHERE number = ?`, number)
	inv, err := scanInvoice(row.Scan)
	if err != nil {
		return nil, err
	}
	items, err := r.itemsFor(ctx, inv.ID)
	if err != nil {
		return nil, fmt.Errorf("get invoice by number: %w", err)
	}
	inv.Items = items
	return inv, nil
}

// UpdateStatus sets the status directly; transitions are any->any by design.
// The draft->sent item requirement is enforced at Create because items are
// immutable post-create.
func (r *InvoiceRepo) UpdateStatus(ctx context.Context, id string, status string) error {
	if !validInvoiceStatus(status) {
		return fmt.Errorf("update invoice status: invalid status %q", status)
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE invoices SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("update invoice status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update invoice status: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *InvoiceRepo) List(ctx context.Context) ([]*model.Invoice, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+invoiceCols+` FROM invoices ORDER BY number DESC`)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()

	invoices := make([]*model.Invoice, 0)
	for rows.Next() {
		inv, err := scanInvoice(rows.Scan)
		if err != nil {
			return nil, err
		}
		invoices = append(invoices, inv)
	}
	return invoices, rows.Err()
}

func (r *InvoiceRepo) ListByStatus(ctx context.Context, statuses ...string) ([]*model.Invoice, error) {
	if len(statuses) == 0 {
		return nil, fmt.Errorf("list invoices by status: no statuses provided")
	}
	for _, s := range statuses {
		if !validInvoiceStatus(s) {
			return nil, fmt.Errorf("list invoices by status: unknown status %q (valid: %s)", s, strings.Join(invoiceStatuses, ", "))
		}
	}
	args := make([]any, len(statuses))
	for i, s := range statuses {
		args[i] = s
	}
	query := `SELECT ` + invoiceCols + ` FROM invoices WHERE status IN (` +
		strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") +
		`) ORDER BY number DESC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list invoices by status: %w", err)
	}
	defer rows.Close()

	invoices := make([]*model.Invoice, 0)
	for rows.Next() {
		inv, err := scanInvoice(rows.Scan)
		if err != nil {
			return nil, err
		}
		invoices = append(invoices, inv)
	}
	return invoices, rows.Err()
}

func (r *InvoiceRepo) ListByClient(ctx context.Context, clientID string) ([]*model.Invoice, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+invoiceCols+` FROM invoices WHERE client_id = ? ORDER BY number DESC`, clientID)
	if err != nil {
		return nil, fmt.Errorf("list invoices by client: %w", err)
	}
	defer rows.Close()

	invoices := make([]*model.Invoice, 0)
	for rows.Next() {
		inv, err := scanInvoice(rows.Scan)
		if err != nil {
			return nil, err
		}
		invoices = append(invoices, inv)
	}
	return invoices, rows.Err()
}

// CloneFromTemplate loads a template with its items and creates a new draft
// invoice copying client, pix key, notes and item lines. The creation runs in
// a single transaction (via Create); the template is left untouched.
func (r *InvoiceRepo) CloneFromTemplate(ctx context.Context, templateID string, issueDate, dueDate time.Time) (*model.Invoice, error) {
	tpl, err := r.Get(ctx, templateID)
	if err != nil {
		return nil, fmt.Errorf("clone invoice from template %s: %w", templateID, err)
	}
	clone := &model.Invoice{
		ClientID:  tpl.ClientID,
		Status:    "draft",
		IssueDate: issueDate,
		DueDate:   dueDate,
		Notes:     tpl.Notes,
	}
	if tpl.PIXKey != nil {
		pix := *tpl.PIXKey
		clone.PIXKey = &pix
	}
	for _, ti := range tpl.Items {
		item := &model.InvoiceItem{
			Description: ti.Description,
			UnitPrice:   ti.UnitPrice,
			Quantity:    ti.Quantity,
			Total:       ti.UnitPrice * ti.Quantity,
		}
		if ti.ProductID != nil {
			pid := *ti.ProductID
			item.ProductID = &pid
		}
		clone.Items = append(clone.Items, item)
	}
	if err := r.Create(ctx, clone); err != nil {
		return nil, fmt.Errorf("clone invoice from template %s: %w", templateID, err)
	}
	return clone, nil
}

// itemsFor loads an invoice's items in insertion order (rowid order matches
// the sequential inserts done at creation time).
func (r *InvoiceRepo) itemsFor(ctx context.Context, invoiceID string) ([]*model.InvoiceItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+invoiceItemCols+` FROM invoice_items WHERE invoice_id = ? ORDER BY rowid`, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*model.InvoiceItem, 0)
	for rows.Next() {
		var it model.InvoiceItem
		if err := rows.Scan(&it.ID, &it.InvoiceID, &it.ProductID, &it.Description, &it.UnitPrice, &it.Quantity, &it.Total); err != nil {
			return nil, err
		}
		items = append(items, &it)
	}
	return items, rows.Err()
}

func scanInvoice(scan func(dest ...any) error) (*model.Invoice, error) {
	var inv model.Invoice
	err := scan(&inv.ID, &inv.ClientID, &inv.Number, &inv.Status, &inv.IssueDate, &inv.DueDate, &inv.Subtotal, &inv.Total, &inv.Notes, &inv.PIXKey, &inv.PDFPath, &inv.CreatedAt, &inv.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}
