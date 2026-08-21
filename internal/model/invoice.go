package model

import "time"

type Invoice struct {
	ID        string
	ClientID  string
	Number    int64
	Status    string // draft|sent|paid|overdue|cancelled
	IssueDate time.Time
	DueDate   time.Time
	Subtotal  int64 // cents, computed from items
	Total     int64 // cents
	Notes     string
	PIXKey    *string
	PDFPath   *string
	Items     []*InvoiceItem // populated on Get; ignored on insert (use Items field of Create input)
	CreatedAt time.Time
	UpdatedAt time.Time
}

type InvoiceItem struct {
	ID          string
	InvoiceID   string
	ProductID   *string
	Description string
	UnitPrice   int64
	Quantity    int64
	Total       int64 // unit_price * quantity
}
