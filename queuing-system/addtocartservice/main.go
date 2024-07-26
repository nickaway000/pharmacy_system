package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

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

	http.HandleFunc("/addtocart", addToCart)

	fmt.Printf("Starting server at port 9001\n")
	log.Fatal(http.ListenAndServe(":9001", nil))
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

func addToCart(w http.ResponseWriter, r *http.Request) {
	log.Print("addtocart invoked")

	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	userIDCookie, err := r.Cookie("userID")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, err := strconv.Atoi(userIDCookie.Value)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var item CartItem
	err = json.NewDecoder(r.Body).Decode(&item)
	if err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	var availableQuantity int
	err = db.QueryRow("SELECT quantity FROM products WHERE id = $1 FOR UPDATE", item.ProductID).Scan(&availableQuantity)
	if err != nil {
		log.Printf("Product not found or error fetching quantity: %v", err)
		http.Error(w, "Product not found", http.StatusNotFound)
		return
	}

	if availableQuantity < item.Quantity {
		log.Printf("Not enough stock available: requested %d, available %d", item.Quantity, availableQuantity)
		http.Error(w, "Not enough stock available", http.StatusConflict)
		return
	}

	_, err = db.Exec("INSERT INTO cart (user_id, product_id, quantity) VALUES ($1, $2, $3)", userID, item.ProductID, item.Quantity)
	if err != nil {
		log.Printf("Failed to add item to cart: %v", err)
		http.Error(w, "Failed to add item to cart", http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully added item to cart for user %d", userID)
	response := map[string]string{"message": "Item successfully added to cart"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
