package repo

import (
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("not found")

type Repos struct {
	Clients *ClientRepo
}

func New(db *sql.DB) *Repos {
	return &Repos{
		Clients: &ClientRepo{db: db},
	}
}
