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

	"github.com/joho/godotenv"

	_ "github.com/lib/pq"
)

var db *sql.DB
var mu sync.Mutex

type Order struct {
	UserID    int        `json:"user_id"`
	EmailID   string     `json:"email_id"`
	Cart      []CartItem `json:"cart"`
	OrderDate time.Time  `json:"order_date"`
}

type CartItem struct {
	UserID    int    `json:"user_id"`
	EmailID   string `json:"email_id"`
	ProductID int    `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

func main() {
	var err error

	err = godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	err = InitDB()
	if err != nil {
		log.Fatalf("Error initializing database: %v", err)
	}

	http.Handle("/", http.FileServer(http.Dir("./static")))

	http.HandleFunc("/remove", removeDB)
	http.HandleFunc("/rollback", rollbackRemoveDB)

	fmt.Printf("Starting server at port 8007\n")
	log.Fatal(http.ListenAndServe(":8007", nil))
}

func InitDB() error {
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"))

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("error connecting to the database: %w", err)
	}

	err = db.Ping()
	if err != nil {
		return fmt.Errorf("error pinging the database: %w", err)
	}

	log.Println("Successfully connected to the database")
	return nil
}

func removeDB(w http.ResponseWriter, r *http.Request) {
	log.Print("removeDB invoked")

	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var order Order
	err := json.NewDecoder(r.Body).Decode(&order)
	if err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	order.OrderDate = time.Now()

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Failed to begin transaction", http.StatusInternalServerError)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	for _, item := range order.Cart {
		var availableQuantity int
		err = tx.QueryRow("SELECT quantity FROM products WHERE id = $1", item.ProductID).Scan(&availableQuantity)
		if err != nil {
			tx.Rollback()
			http.Error(w, "Product not found", http.StatusInternalServerError)
			return
		}
		if availableQuantity < item.Quantity {
			tx.Rollback()
			http.Error(w, fmt.Sprintf("Insufficient quantity for product ID %d", item.ProductID), http.StatusBadRequest)
			return
		}

		_, err = tx.Exec("UPDATE products SET quantity = quantity - $1 WHERE id = $2",
			item.Quantity, item.ProductID)
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to update product stock: %v", err)
			http.Error(w, "Failed to update product stock", http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec("DELETE FROM cart WHERE user_id = $1 AND product_id = $2",
			order.UserID, item.ProductID)
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to remove item from cart: %v", err)
			http.Error(w, "Failed to remove item from cart", http.StatusInternalServerError)
			return
		}
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("Failed to commit transaction: %v", err)
		http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
		return
	}

	response := map[string]string{"message": "Items successfully removed from db"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func rollbackRemoveDB(w http.ResponseWriter, r *http.Request) {
	log.Print("rollbackRemoveDB invoked")

	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var cartItems []CartItem
	err := json.NewDecoder(r.Body).Decode(&cartItems)
	if err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Failed to begin transaction", http.StatusInternalServerError)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	for _, item := range cartItems {
		_, err = tx.Exec("UPDATE products SET quantity = quantity + $1 WHERE id = $2", item.Quantity, item.ProductID)
		if err != nil {
			tx.Rollback()
			http.Error(w, "Failed to rollback product stock", http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec("INSERT INTO cart (user_id, product_id, quantity) VALUES ($1, $2, $3)",
			item.UserID, item.ProductID, item.Quantity)
		if err != nil {
			tx.Rollback()
			http.Error(w, "Failed to add item back to cart", http.StatusInternalServerError)
			return
		}
	}

	err = tx.Commit()
	if err != nil {
		http.Error(w, "Failed to commit rollback transaction", http.StatusInternalServerError)
		return
	}

	response := map[string]string{"message": "Items successfully rolled back to cart"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
