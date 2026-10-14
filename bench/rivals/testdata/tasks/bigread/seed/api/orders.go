package api

import (
	"encoding/json"
	"net/http"

	"example.com/atlas/classify"
	"example.com/atlas/intake"
	"example.com/atlas/pipeline"
)

func HandleOrder(w http.ResponseWriter, r *http.Request) {
	var o intake.Order
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	o.Class = classify.FromHeader(r.Header.Get("X-Priority"))
	if err := pipeline.Default().Submit(&o); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func HandleQuote(w http.ResponseWriter, r *http.Request) {
	var o intake.Order
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	o.Class = classify.FromQuery(r.URL.Query())
	if err := pipeline.Quotes().Submit(&o); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
