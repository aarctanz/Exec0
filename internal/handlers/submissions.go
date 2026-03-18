package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/aarctanz/Exec0/internal/models/submissions"
	"github.com/aarctanz/Exec0/internal/util"
	"github.com/aarctanz/Exec0/internal/services"
)

type SubmissionsHandler struct {
	service *services.SubmissionsService
}

func NewSubmissionsHandler(service *services.SubmissionsService) *SubmissionsHandler {
	return &SubmissionsHandler{service: service}
}

func (h *SubmissionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var dto submissions.CreateSubmissionDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	id, err := h.service.CreateSubmission(dto)
	if err != nil {
		util.Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	util.JSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (h *SubmissionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		util.Error(w, http.StatusBadRequest, "invalid submission id")
		return
	}

	sub, err := h.service.GetSubmissionById(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "submission not found")
		return
	}
	util.JSON(w, http.StatusOK, sub)
}
