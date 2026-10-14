package api

import "net/http"

func Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/orders", HandleOrder)
	mux.HandleFunc("/quotes", HandleQuote)
	return mux
}
