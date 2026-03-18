package handlers

import (
	"net/http"

	"github.com/aarctanz/Exec0/internal/util"
)

func Health(w http.ResponseWriter, r *http.Request) {
	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
