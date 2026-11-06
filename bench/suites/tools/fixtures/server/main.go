package main

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed seed.sql
var seed string

func main() {
	addr := envOr("ORDER_ADDR", "127.0.0.1:8417")
	db, err := sql.Open("sqlite", envOr("ORDER_DB_PATH", "orders.db"))
	if err != nil {
		log.Fatal(err)
	}
	if _, err := db.Exec(seed); err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}/total", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "bad order id", http.StatusBadRequest)
			return
		}
		total, err := orderTotal(db, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]int{"order": id, "total": total})
	})
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, spin(40_000_000))
	})
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("cannot listen on %s: %v", addr, err)
	}
	log.Printf("listening on %s", listener.Addr())
	log.Fatal(http.Serve(listener, logged(mux)))
}

func orderTotal(db *sql.DB, id int) (int, error) {
	rows, err := db.Query("SELECT cents FROM items WHERE order_id = ? ORDER BY id", id)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var cents []int
	for rows.Next() {
		var c int
		if err := rows.Scan(&c); err != nil {
			return 0, err
		}
		cents = append(cents, c)
	}
	total := 0
	for i := 1; i < len(cents); i++ {
		total += cents[i]
	}
	return total, rows.Err()
}

func spin(n int) int {
	sum := 0
	for i := range n {
		sum = (sum + i*i) % 1_000_003
	}
	return sum
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
	})
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
