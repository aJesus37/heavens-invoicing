package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ajesus37/heavens-invoicing/internal/model"
)

type RecurringRepo struct {
	db *sql.DB
}

const recurringCols = `id, client_id, invoice_template_id, frequency, next_send_date, last_sent_date, delivery_method, active, created_at, updated_at`

var (
	frequencies     = []string{"weekly", "monthly", "quarterly", "yearly"}
	deliveryMethods = []string{"email", "whatsapp", "telegram", "all"}
)

func validFrequency(f string) bool      { return slices.Contains(frequencies, f) }
func validDeliveryMethod(m string) bool { return slices.Contains(deliveryMethods, m) }

func joinOr(values []string) string { return strings.Join(values, ", ") }

// Create inserts a schedule and always starts it active (deactivate via
// Update), mirroring how products are created.
func (r *RecurringRepo) Create(ctx context.Context, s *model.RecurringSchedule) error {
	if !validFrequency(s.Frequency) {
		return fmt.Errorf("create recurring schedule: invalid frequency %q (valid: %s)", s.Frequency, joinOr(frequencies))
	}
	if !validDeliveryMethod(s.DeliveryMethod) {
		return fmt.Errorf("create recurring schedule: invalid delivery method %q (valid: %s)", s.DeliveryMethod, joinOr(deliveryMethods))
	}
	if s.ID == "" {
		s.ID = model.NewID()
	}
	now := time.Now().UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = now
	}
	s.Active = true
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO recurring_schedules (`+recurringCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.ClientID, s.InvoiceTemplateID, s.Frequency, s.NextSendDate, s.LastSentDate, s.DeliveryMethod, s.Active, s.CreatedAt, s.UpdatedAt,
	); err != nil {
		return fmt.Errorf("create recurring schedule: %w", err)
	}
	return nil
}

func (r *RecurringRepo) Get(ctx context.Context, id string) (*model.RecurringSchedule, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+recurringCols+` FROM recurring_schedules WHERE id = ?`, id)
	return scanRecurring(row.Scan)
}

func (r *RecurringRepo) Update(ctx context.Context, s *model.RecurringSchedule) error {
	if !validFrequency(s.Frequency) {
		return fmt.Errorf("update recurring schedule: invalid frequency %q (valid: %s)", s.Frequency, joinOr(frequencies))
	}
	if !validDeliveryMethod(s.DeliveryMethod) {
		return fmt.Errorf("update recurring schedule: invalid delivery method %q (valid: %s)", s.DeliveryMethod, joinOr(deliveryMethods))
	}
	s.UpdatedAt = time.Now().UTC()
	res, err := r.db.ExecContext(ctx,
		`UPDATE recurring_schedules SET client_id = ?, invoice_template_id = ?, frequency = ?, next_send_date = ?, last_sent_date = ?, delivery_method = ?, active = ?, updated_at = ? WHERE id = ?`,
		s.ClientID, s.InvoiceTemplateID, s.Frequency, s.NextSendDate, s.LastSentDate, s.DeliveryMethod, s.Active, s.UpdatedAt, s.ID)
	if err != nil {
		return fmt.Errorf("update recurring schedule: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update recurring schedule: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns every schedule ordered by the date the scheduler fires on.
func (r *RecurringRepo) List(ctx context.Context) ([]*model.RecurringSchedule, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+recurringCols+` FROM recurring_schedules ORDER BY next_send_date`)
	if err != nil {
		return nil, fmt.Errorf("list recurring schedules: %w", err)
	}
	defer rows.Close()

	schedules := make([]*model.RecurringSchedule, 0)
	for rows.Next() {
		s, err := scanRecurring(rows.Scan)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

// ListActive returns every active schedule ordered by fire date; this is
// the query the scheduler ticks against.
func (r *RecurringRepo) ListActive(ctx context.Context) ([]*model.RecurringSchedule, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+recurringCols+` FROM recurring_schedules WHERE active = 1 ORDER BY next_send_date`)
	if err != nil {
		return nil, fmt.Errorf("list active recurring schedules: %w", err)
	}
	defer rows.Close()

	schedules := make([]*model.RecurringSchedule, 0)
	for rows.Next() {
		s, err := scanRecurring(rows.Scan)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

func (r *RecurringRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM recurring_schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete recurring schedule: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete recurring schedule: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanRecurring(scan func(dest ...any) error) (*model.RecurringSchedule, error) {
	var s model.RecurringSchedule
	err := scan(&s.ID, &s.ClientID, &s.InvoiceTemplateID, &s.Frequency, &s.NextSendDate, &s.LastSentDate, &s.DeliveryMethod, &s.Active, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}
