package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"seller-ops/server/internal/auth"
	"seller-ops/server/internal/httpresponse"
)

func (s *server) businessBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	if r.Method != "POST" && r.Method != "PUT" && r.Method != "PATCH" {
		return nil, true
	}
	var b map[string]any
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	d.UseNumber()
	if err := d.Decode(&b); err != nil {
		httpresponse.Fail(w, 400, "invalid request")
		return nil, false
	}
	return b, true
}
func (s *server) respondBusiness(w http.ResponseWriter, result *auth.Result, err error) {
	if err != nil {
		var e auth.APIError
		if errors.As(err, &e) {
			httpresponse.Fail(w, e.Status, e.Message)
		} else {
			httpresponse.Internal(w, err)
		}
		return
	}
	if result == nil {
		httpresponse.Internal(w, errors.New("missing result"))
		return
	}
	if result.Status == 204 {
		w.WriteHeader(204)
		return
	}
	httpresponse.JSON(w, result.Status, result.Data)
}
