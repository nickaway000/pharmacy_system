package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type Order struct {
	UserID  int       `json:"user_id"`
	EmailID string    `json:"email_id"`
	Cart    []CartItem `json:"cart"`
}

type CartItem struct {
	ProductID int `json:"product_id"`
	Quantity  int `json:"quantity"`
}

func main() {
	http.HandleFunc("/payment", paymentHandler)

	fmt.Println("Starting payment service at port 8006")
	log.Fatal(http.ListenAndServe(":8006", nil))
}

func paymentHandler(w http.ResponseWriter, r *http.Request) {
	log.Print("paymentHandler invoked")

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

	// Simulate a successful payment
	response := map[string]string{
		"message": "Payment successful",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
