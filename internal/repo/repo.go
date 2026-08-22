package repo

import (
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("not found")

type Repos struct {
	Clients   *ClientRepo
	Products  *ProductRepo
	Invoices  *InvoiceRepo
	Settings  *SettingsRepo
	Recurring *RecurringRepo
}

func New(db *sql.DB) *Repos {
	return &Repos{
		Clients:   &ClientRepo{db: db},
		Products:  &ProductRepo{db: db},
		Invoices:  &InvoiceRepo{db: db},
		Settings:  &SettingsRepo{db: db},
		Recurring: &RecurringRepo{db: db},
	}
}
