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

type OrderResponse struct {
	ID              string    `json:"id"`
	CustomerName    string    `json:"customer_name"`
	ItemName        string    `json:"item_name"`
	DeliveryAddress string    `json:"delivery_address"`
	CreatedAt       time.Time `json:"created_at"`
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

	// Start background initialization of default data
	go func() {
		time.Sleep(5 * time.Second) // Wait for DDL setup completed by spanner-init
		initDefaultData()
	}()

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
		CustomerName    string `json:"customer_name"`
		ItemName        string `json:"item_name"`
		DeliveryAddress string `json:"delivery_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.CustomerName == "" || req.ItemName == "" || req.DeliveryAddress == "" {
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
		id, req.CustomerName, req.DeliveryAddress, createdAt)
	if err != nil {
		log.Printf("Insert order error: %v\n", err)
		http.Error(w, "Failed to save order to database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Insert child order_item record (Interleaved)
	itemID := uuid.New().String()
	_, err = tx.Exec("INSERT INTO order_items (id, item_id, item_name, quantity) VALUES ($1, $2, $3, $4)",
		id, itemID, req.ItemName, 1)
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

	var contacts []OrderResponse
	for rows.Next() {
		var c OrderResponse
		if err := rows.Scan(&c.ID, &c.CustomerName, &c.ItemName, &c.DeliveryAddress, &c.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		contacts = append(contacts, c)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contacts)
}

func initDefaultData() {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&count)
	if err != nil {
		log.Printf("Failed to count orders during init: %v. Database schema might not be ready yet. Skipping init.\n", err)
		return
	}
	if count > 0 {
		log.Println("Default data already exists. Skipping insertion.")
		return
	}

	log.Println("Database is empty. Inserting default Spanner-themed orders...")
	
	defaultOrders := []struct {
		ID       string
		Name     string
		MenuItem string
		Address  string
		TimeDiff time.Duration
	}{
		{
			ID:       "ef3ea497-de0c-4aea-8978-a0b7cb874a35",
			Name:     "山田 太郎",
			MenuItem: "特上江戸前寿司 (3人前)",
			Address:  "GCP県スパーナー市マルチリージョン1-1-1 スパーナービル501号 / インターホンを鳴らさずに置き配でお願いします。",
			TimeDiff: -3 * time.Minute,
		},
		{
			ID:       "f8da8be9-af41-449f-8ccd-8c6a59630a96",
			Name:     "鈴木 花子",
			MenuItem: "特製濃厚デミグラスハンバーグ弁当 (2人前)",
			Address:  "クラウド県サーバーレス町インスタンス2-3-4 / 到着前にお電話ください。",
			TimeDiff: -2 * time.Minute,
		},
		{
			ID:       "4d6f571f-1521-446c-b643-3dce79c9420d",
			Name:     "佐藤 健",
			MenuItem: "博多極細豚骨ラーメン＆大餃子セット",
			Address:  "合意形成県パクソス区レプリカ5-5-5 合意タワー303号",
			TimeDiff: -1 * time.Minute,
		},
	}

	for _, o := range defaultOrders {
		tx, err := db.Begin()
		if err != nil {
			log.Printf("Failed to begin init transaction: %v\n", err)
			continue
		}
		
		createdAt := time.Now().Add(o.TimeDiff)
		
		_, err = tx.Exec("INSERT INTO orders (id, customer_name, delivery_address, created_at) VALUES ($1, $2, $3, $4)",
			o.ID, o.Name, o.Address, createdAt)
		if err != nil {
			log.Printf("Init parent order error: %v\n", err)
			tx.Rollback()
			continue
		}

		itemID := uuid.New().String()
		_, err = tx.Exec("INSERT INTO order_items (id, item_id, item_name, quantity) VALUES ($1, $2, $3, $4)",
			o.ID, itemID, o.MenuItem, 1)
		if err != nil {
			log.Printf("Init child order item error: %v\n", err)
			tx.Rollback()
			continue
		}

		if err := tx.Commit(); err != nil {
			log.Printf("Init commit error: %v\n", err)
		} else {
			log.Printf("Inserted default order for %s successfully.\n", o.Name)
		}
	}
}
