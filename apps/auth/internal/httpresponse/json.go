package httpresponse

import (
	"encoding/json"
	"log"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func Fail(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]any{"success": false, "message": message})
}

func Internal(w http.ResponseWriter, err error) {
	log.Print(err)
	Fail(w, 500, "internal server error")
}
