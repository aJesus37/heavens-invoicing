package model

import "time"

type RecurringSchedule struct {
	ID                string
	ClientID          string
	InvoiceTemplateID string
	Frequency         string // weekly|monthly|quarterly|yearly
	NextSendDate      time.Time
	LastSentDate      *time.Time
	DeliveryMethod    string // email|whatsapp|telegram|all
	Active            bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
