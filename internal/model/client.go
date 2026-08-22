package model

import "time"

type Client struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Email          *string   `json:"email,omitempty"`
	Phone          *string   `json:"phone,omitempty"`
	TelegramChatID *string   `json:"telegram_chat_id,omitempty"`
	PIXKey         *string   `json:"pix_key,omitempty"`
	Address        string    `json:"address"`
	Notes          string    `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
