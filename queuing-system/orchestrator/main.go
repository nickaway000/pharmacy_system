package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/joho/godotenv"
	"github.com/gorilla/handlers"
)

var mu sync.Mutex

type CartItem struct {
	ProductID int `json:"product_id"`
	Quantity  int `json:"quantity"`
}

type Order struct {
	UserID  int       `json:"user_id"`
	EmailID string    `json:"email_id"`
	Cart    []CartItem `json:"cart"`
}

func main() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	http.HandleFunc("/confirmorder", confirmOrder)

	corsHandler := handlers.CORS(
		handlers.AllowedOrigins([]string{"http://localhost:9003"}),
		handlers.AllowedMethods([]string{"GET", "POST", "OPTIONS"}),
		handlers.AllowedHeaders([]string{"Content-Type", "Authorization"}),
	)(http.DefaultServeMux)

	fmt.Println("Starting orchestrator service at port 8005")
	log.Fatal(http.ListenAndServe(":8005", corsHandler))
}

func confirmOrder(w http.ResponseWriter, r *http.Request) {
	log.Print("confirmOrder invoked")

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

	if len(order.Cart) == 0 {
		http.Error(w, "No items provided", http.StatusBadRequest)
		return
	}

	// Place the order
	success := callPlaceOrderService(order)
	if !success {
		writeError(w, "Failed to place order", http.StatusInternalServerError)
		return
	}

	// Process payment
	success = callPaymentService(order)
	if !success {
		rollbackPlaceOrderService(order)
		writeError(w, "Failed to process payment", http.StatusInternalServerError)
		return
	}

	// Send notification
	success = callNotificationService(order)
	if !success {
		rollbackPlaceOrderService(order)
		writeError(w, "Failed to send notification", http.StatusInternalServerError)
		return
	}

	// Remove from database
	success = callRemoveDBService(order)
	if !success {
		rollbackPlaceOrderService(order)
		writeError(w, "Failed to remove from DB", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Order confirmed successfully"})
}

func writeError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"message": message})
}

func callPlaceOrderService(order Order) bool {
	url := "http://localhost:9003/placeorder" // Place order service URL

	jsonOrder, err := json.Marshal(order)
	if err != nil {
		log.Printf("Error marshaling order: %v", err)
		return false
	}

	resp, err := makeRequest("POST", url, jsonOrder)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Error calling place order service: %v", err)
		return false
	}

	return true
}

func callPaymentService(order Order) bool {
	url := "http://localhost:8006/payment" // Payment service URL

	jsonOrder, err := json.Marshal(order)
	if err != nil {
		log.Printf("Error marshaling order for payment: %v", err)
		return false
	}

	resp, err := makeRequest("POST", url, jsonOrder)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Error calling payment service: %v", err)
		return false
	}

	return true
}

func callNotificationService(order Order) bool {
	url := "http://localhost:8004/notify" // Notification service URL

	jsonOrder, err := json.Marshal(order)
	if err != nil {
		log.Printf("Error marshaling order for notification: %v", err)
		return false
	}

	resp, err := makeRequest("POST", url, jsonOrder)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Error calling notification service: %v", err)
		return false
	}

	return true
}

func callRemoveDBService(order Order) bool {
	url := "http://localhost:8007/remove" // Remove from DB service URL

	jsonOrder, err := json.Marshal(order)
	if err != nil {
		log.Printf("Error marshaling order for remove DB: %v", err)
		return false
	}

	resp, err := makeRequest("POST", url, jsonOrder)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Error calling remove DB service: %v", err)
		return false
	}

	return true
}

func rollbackPlaceOrderService(order Order) {
	url := "http://localhost:9003/rollback" // Place order rollback URL

	jsonOrder, err := json.Marshal(order)
	if err != nil {
		log.Printf("Error marshaling order for rollback: %v", err)
		return
	}

	resp, err := makeRequest("POST", url, jsonOrder)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Error calling place order rollback service: %v", err)
	}
}

func makeRequest(method, url string, jsonBody []byte) (*http.Response, error) {
	req, err := http.NewRequest(method, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error making request: %v", err)
		return nil, err
	}

	return resp, nil
}
