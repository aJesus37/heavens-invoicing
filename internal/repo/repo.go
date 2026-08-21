package repo

import (
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("not found")

type Repos struct {
	Clients  *ClientRepo
	Products *ProductRepo
	Invoices *InvoiceRepo
}

func New(db *sql.DB) *Repos {
	return &Repos{
		Clients:  &ClientRepo{db: db},
		Products: &ProductRepo{db: db},
		Invoices: &InvoiceRepo{db: db},
	}
}
