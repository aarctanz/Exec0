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

func (h *SubmissionsHandler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))

	subs, err := h.service.ListSubmissions(r.Context(), int32(page), int32(perPage))
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to fetch submissions")
		return
	}
	util.JSON(w, http.StatusOK, subs)
}

func (h *SubmissionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var dto submissions.CreateSubmissionDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	id, err := h.service.CreateSubmission(r.Context(), dto)
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

	sub, err := h.service.GetSubmissionById(r.Context(), id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "submission not found")
		return
	}
	util.JSON(w, http.StatusOK, sub)
}
