package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Contact struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type Region struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"` // "ONLINE" or "OFFLINE"
	Type   string `json:"type"`   // "Leader", "Replica", "Witness"
}

var (
	db        *sql.DB
	regionsMu sync.RWMutex
	regions   = []Region{
		{ID: "ichikawa", Name: "市川リージョン (Leader)", Status: "ONLINE", Type: "Leader"},
		{ID: "wakkanai", Name: "稚内リージョン (Replica)", Status: "ONLINE", Type: "Replica"},
		{ID: "yonaguni", Name: "与那国リージョン (Replica)", Status: "ONLINE", Type: "Replica"},
		{ID: "tokushima", Name: "徳島リージョン (Witness)", Status: "ONLINE", Type: "Witness"},
		{ID: "sado", Name: "佐渡リージョン (Witness)", Status: "ONLINE", Type: "Witness"},
	}
)

func main() {
	var err error
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres@spanner-pgadapter:5432/local-db?sslmode=disable"
	}

	// Retry loop for DB startup
	for i := 0; i < 20; i++ {
		db, err = sql.Open("pgx", connStr)
		if err == nil {
			err = db.Ping()
			if err == nil {
				log.Println("Successfully connected to Spanner via PGAdapter!")
				break
			}
		}
		log.Printf("Waiting for database connection (attempt %d/20)... error: %v\n", i+1, err)
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Static routes
	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/style.css", serveStyle)

	// API routes
	http.HandleFunc("/api/contact", handleContact)
	http.HandleFunc("/api/contacts", handleGetContacts)
	http.HandleFunc("/api/regions", handleGetRegions)
	http.HandleFunc("/api/regions/toggle", handleToggleRegion)

	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}
	log.Printf("Go Server starting on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "public/index.html")
}

func serveStyle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	http.ServeFile(w, r, "public/style.css")
}

func handleGetRegions(w http.ResponseWriter, r *http.Request) {
	regionsMu.RLock()
	defer regionsMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(regions)
}

func handleToggleRegion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	regionsMu.Lock()
	defer regionsMu.Unlock()

	found := false
	for i, reg := range regions {
		if reg.ID == req.ID {
			found = true
			if reg.Status == "ONLINE" {
				regions[i].Status = "OFFLINE"
			} else {
				regions[i].Status = "ONLINE"
			}
			break
		}
	}

	if !found {
		http.Error(w, "Region not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "regions": regions})
}

func handleContact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name    string `json:"name"`
		Email   string `json:"email"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Email == "" || req.Message == "" {
		http.Error(w, "All fields are required", http.StatusBadRequest)
		return
	}

	// Evaluate Paxos Quorum
	regionsMu.RLock()
	totalCount := len(regions)
	quorumRequired := (totalCount / 2) + 1
	onlineCount := 0
	paxosStatus := make(map[string]string)
	for _, reg := range regions {
		if reg.Status == "ONLINE" {
			onlineCount++
			paxosStatus[reg.ID] = "ACK"
		} else {
			paxosStatus[reg.ID] = "TIMEOUT"
		}
	}
	regionsMu.RUnlock()

	quorumReached := onlineCount >= quorumRequired

	if !quorumReached {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict) // 409 Conflict
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Paxos consensus failed: Quorum lost (less than %d/%d replicas online).", quorumRequired, totalCount),
			"paxos":   paxosStatus,
		})
		return
	}

	id := uuid.New().String()
	createdAt := time.Now()

	// Insert order and order_items via PGAdapter inside a transaction (PG-dialect)
	tx, err := db.Begin()
	if err != nil {
		log.Printf("Transaction begin error: %v\n", err)
		http.Error(w, "Failed to start transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// 1. Insert parent order record
	_, err = tx.Exec("INSERT INTO orders (id, customer_name, delivery_address, created_at) VALUES ($1, $2, $3, $4)",
		id, req.Name, req.Message, createdAt)
	if err != nil {
		log.Printf("Insert order error: %v\n", err)
		http.Error(w, "Failed to save order to database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Insert child order_item record (Interleaved)
	itemID := uuid.New().String()
	_, err = tx.Exec("INSERT INTO order_items (id, item_id, item_name, quantity) VALUES ($1, $2, $3, $4)",
		id, itemID, req.Email, 1)
	if err != nil {
		log.Printf("Insert order item error: %v\n", err)
		http.Error(w, "Failed to save order item to database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. Commit the transaction
	if err := tx.Commit(); err != nil {
		log.Printf("Transaction commit error: %v\n", err)
		http.Error(w, "Failed to commit transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"id":      id,
		"paxos":   paxosStatus,
	})
}

func handleGetContacts(w http.ResponseWriter, r *http.Request) {
	// Query parent and child records using JOIN (PG-dialect)
	rows, err := db.Query(`
		SELECT o.id, o.customer_name, oi.item_name, o.delivery_address, o.created_at
		FROM orders o
		JOIN order_items oi ON o.id = oi.id
		ORDER BY o.created_at DESC
	`)
	if err != nil {
		log.Printf("Query error: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Message, &c.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		contacts = append(contacts, c)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contacts)
}
