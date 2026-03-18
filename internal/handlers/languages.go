package handlers

import (
	"net/http"
	"strconv"

	"github.com/aarctanz/Exec0/internal/util"
	"github.com/aarctanz/Exec0/internal/services"
)

type LanguagesHandler struct {
	service *services.LanguagesService
}

func NewLanguagesHandler(service *services.LanguagesService) *LanguagesHandler {
	return &LanguagesHandler{service: service}
}

func (h *LanguagesHandler) List(w http.ResponseWriter, r *http.Request) {
	languages, err := h.service.GetPublicLanguages()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to fetch languages")
		return
	}
	util.JSON(w, http.StatusOK, languages)
}

func (h *LanguagesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		util.Error(w, http.StatusBadRequest, "invalid language id")
		return
	}

	language, err := h.service.GetPublicLanguageByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "language not found")
		return
	}
	util.JSON(w, http.StatusOK, language)
}
