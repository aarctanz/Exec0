package services

import "github.com/aarctanz/Exec0/internal/database/queries"

type Services struct {
}

func New(queries *queries.Queries) *Services {
	return &Services{}
}
