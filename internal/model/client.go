package model

import "time"

type Client struct {
	ID             string
	Name           string
	Email          *string
	Phone          *string
	TelegramChatID *string
	PIXKey         *string
	Address        string
	Notes          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
