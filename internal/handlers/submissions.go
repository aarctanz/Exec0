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

	testCases, err := h.service.GetTestCaseResults(r.Context(), int64(id))
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to fetch test case results")
		return
	}

	util.JSON(w, http.StatusOK, map[string]any{
		"id":              sub.ID,
		"language_id":     sub.LanguageID,
		"source_code":     sub.SourceCode,
		"mode":            sub.Mode,
		"status":          sub.Status,
		"compile_output":  sub.CompileOutput.String,
		"message":         sub.Message.String,
		"time":            sub.Time.Float64,
		"wall_time":       sub.WallTime.Float64,
		"memory":          sub.Memory.Int32,
		"started_at":      sub.StartedAt.Time,
		"finished_at":     sub.FinishedAt.Time,
		"created_at":      sub.CreatedAt.Time,
		"updated_at":      sub.UpdatedAt.Time,
		"test_cases":      testCases,
		// Flatten first test case fields for backward compatibility (single mode)
		"stdout":    firstTCField(testCases, func(tc map[string]any) any { return tc["stdout"] }),
		"stderr":    firstTCField(testCases, func(tc map[string]any) any { return tc["stderr"] }),
		"exit_code": firstTCField(testCases, func(tc map[string]any) any { return tc["exit_code"] }),
	})
}

func firstTCField(testCases []map[string]any, fn func(map[string]any) any) any {
	if len(testCases) > 0 {
		return fn(testCases[0])
	}
	return nil
}
