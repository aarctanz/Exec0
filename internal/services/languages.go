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

func (s *LanguagesService) GetLanguages() ([]queries.ListLanguagesRow, error) {
	languages, err := s.queries.ListLanguages(context.Background())
	if err != nil {
		return nil, err
	}
	return languages, nil
}

func (s *LanguagesService) GetPublicLanguages() ([]queries.ListPublicLanguagesRow, error) {
	languages, err := s.queries.ListPublicLanguages(context.Background())
	if err != nil {
		return nil, err
	}
	return languages, nil
}

func (s *LanguagesService) GetLanguageByID(id int64) (queries.GetPublicLanguageByIDRow, error) {
	language, err := s.queries.GetPublicLanguageByID(context.Background(), id)
	if err != nil {
		return queries.GetPublicLanguageByIDRow{}, err
	}
	return language, nil
}

func (s *LanguagesService) GetPublicLanguageByID(id int64) (queries.GetPublicLanguageByIDRow, error) {
	language, err := s.queries.GetPublicLanguageByID(context.Background(), id)
	if err != nil {
		return queries.GetPublicLanguageByIDRow{}, err
	}
	return language, nil
}
