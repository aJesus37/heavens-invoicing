package model

import "time"

type Product struct {
	ID          string
	Name        string
	Description string
	UnitPrice   int64 // cents
	Currency    string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
