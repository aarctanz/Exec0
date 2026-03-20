package services

import (
	"context"

	"github.com/aarctanz/Exec0/internal/database/queries"
)

type LanguagesService struct {
	queries *queries.Queries
}

func NewLanguagesService(queries *queries.Queries) *LanguagesService {
	return &LanguagesService{queries: queries}
}

func (s *LanguagesService) GetLanguages(ctx context.Context) ([]queries.ListLanguagesRow, error) {
	languages, err := s.queries.ListLanguages(ctx)
	if err != nil {
		return nil, err
	}
	return languages, nil
}

func (s *LanguagesService) GetPublicLanguages(ctx context.Context) ([]queries.ListPublicLanguagesRow, error) {
	languages, err := s.queries.ListPublicLanguages(ctx)
	if err != nil {
		return nil, err
	}
	return languages, nil
}

func (s *LanguagesService) GetLanguageByID(ctx context.Context, id int64) (queries.GetPublicLanguageByIDRow, error) {
	language, err := s.queries.GetPublicLanguageByID(ctx, id)
	if err != nil {
		return queries.GetPublicLanguageByIDRow{}, err
	}
	return language, nil
}

func (s *LanguagesService) GetPublicLanguageByID(ctx context.Context, id int64) (queries.GetPublicLanguageByIDRow, error) {
	language, err := s.queries.GetPublicLanguageByID(ctx, id)
	if err != nil {
		return queries.GetPublicLanguageByIDRow{}, err
	}
	return language, nil
}
