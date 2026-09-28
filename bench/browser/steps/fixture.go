package steps

import (
	"embed"
	"io/fs"
	"net"
	"net/http"
	"time"
)

const (
	slowNavigation    = 800 * time.Millisecond
	readHeaderTimeout = 5 * time.Second
)

//go:embed fixture
var fixture embed.FS

func Serve() (string, func() error, error) {
	pages, err := fs.Sub(fixture, "fixture")
	if err != nil {
		return "", nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServerFS(pages))
	mux.HandleFunc("/booked", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(slowNavigation)
		http.ServeFileFS(w, r, pages, "booked.html")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: readHeaderTimeout}
	go func() { _ = server.Serve(listener) }()
	return "http://" + listener.Addr().String() + "/", server.Close, nil
}
