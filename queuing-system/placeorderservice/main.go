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

type CartItem struct {
	UserID    int    `json:"user_id"`
	EmailID   string `json:"email_id"`
	ProductID int    `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type Order struct {
	UserID    int       `json:"user_id"`
	EmailID   string    `json:"email_id"`
	Cart      []CartItem `json:"cart"`
	OrderDate time.Time `json:"order_date"`
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

	http.HandleFunc("/placeorder", placeOrder)
	http.HandleFunc("/rollback", rollbackOrder)
	http.HandleFunc("/cart", getCart)
	http.HandleFunc("/cancel", cancelCart)

	fmt.Printf("Starting server at port 9003\n")
	log.Fatal(http.ListenAndServe(":9003", nil))
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

func getCart(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userID")
	if userID == "" {
		http.Error(w, "Missing userID parameter", http.StatusBadRequest)
		return
	}

	rows, err := db.Query("SELECT product_id, quantity FROM cart WHERE user_id = $1", userID)
	if err != nil {
		http.Error(w, "Error fetching cart items", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var cartItems []CartItem
	for rows.Next() {
		var item CartItem
		if err := rows.Scan(&item.ProductID, &item.Quantity); err != nil {
			http.Error(w, "Error scanning cart item", http.StatusInternalServerError)
			return
		}
		cartItems = append(cartItems, item)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cartItems)
}

func cancelCart(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userID")
	if userID == "" {
		http.Error(w, "Missing userID parameter", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("DELETE FROM cart WHERE user_id = $1", userID)
	if err != nil {
		http.Error(w, "Error deleting cart items", http.StatusInternalServerError)
		return
	}

	response := map[string]string{
		"message": "Cart items deleted successfully!",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func placeOrder(w http.ResponseWriter, r *http.Request) {
	log.Print("placeOrder invoked")

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

		_, err = tx.Exec("INSERT INTO orders (user_id, product_id, quantity, email, order_date) VALUES ($1, $2, $3, $4, $5)",
			order.UserID, item.ProductID, item.Quantity, order.EmailID, order.OrderDate)
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to place order: %v", err)
			http.Error(w, "Failed to place order", http.StatusInternalServerError)
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

	response := map[string]string{
		"message": "Order placed successfully!",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func rollbackOrder(w http.ResponseWriter, r *http.Request) {
	log.Print("rollbackOrder invoked")

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

	// Debugging: Print the decoded order details
	log.Printf("Decoded order: %+v", order)

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Failed to begin transaction", http.StatusInternalServerError)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	for _, item := range order.Cart {
		// Debugging: Print each item before processing
		log.Printf("Processing rollback for item: %+v", item)

		_, err = tx.Exec("DELETE FROM orders WHERE user_id = $1 AND product_id = $2 AND email = $3",
			order.UserID, item.ProductID, order.EmailID)
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to rollback order: %v", err)
			http.Error(w, "Failed to rollback order", http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec("INSERT INTO cart (user_id, product_id, quantity) VALUES ($1, $2, $3)",
			order.UserID, item.ProductID, item.Quantity)
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to add item back to cart: %v", err)
			http.Error(w, "Failed to add item back to cart", http.StatusInternalServerError)
			return
		}
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("Failed to commit rollback transaction: %v", err)
		http.Error(w, "Failed to commit rollback transaction", http.StatusInternalServerError)
		return
	}

	response := map[string]string{
		"message": "Order rolled back successfully!",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
