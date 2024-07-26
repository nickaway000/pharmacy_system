package main

import (
    "database/sql"
    "fmt"
    "log"
    "net/http"
    "os"
    "time"

    "github.com/golang-jwt/jwt/v5"
    "github.com/joho/godotenv"
    "golang.org/x/crypto/bcrypt"
    _ "github.com/lib/pq"
)

var db *sql.DB
var jwtKey = []byte(os.Getenv("JWT_SECRET"))


func main() {
    var err error

    err = godotenv.Load(".env")
    if err != nil {
        log.Fatalf("Error loading .env file: %v", err)
    }

    err2 := InitDB()
    if err2 != nil {
        log.Fatalf("Error initializing database: %v", err2)
    }

	http.Handle("/", http.FileServer(http.Dir("./static")))

    http.HandleFunc("/register", RegisterHandler)
    http.HandleFunc("/login", LoginHandler)

    fmt.Printf("Starting server at port 9000\n")
    log.Fatal(http.ListenAndServe(":9000", nil))
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

    errPing := db.Ping()
    if errPing != nil {
        return fmt.Errorf("error pinging the database: %w", errPing)
    }

    log.Println("Successfully connected to the database")
    return nil
}

func generateJWT(userID int) (string, error) {
    claims := &jwt.RegisteredClaims{
        ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
        Issuer:    fmt.Sprintf("%d", userID),
    }

    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    tokenString, err := token.SignedString(jwtKey)
    if err != nil {
        return "", err
    }

    return tokenString, nil
}

// Hash password using bcrypt
func hashPassword(password string) (string, error) {
    bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
    return string(bytes), err
}

// Compare hashed password with plain text
func checkPasswordHash(password, hash string) bool {
    err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
    return err == nil
}

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
        return
    }

    err := r.ParseForm()
    if err != nil {
        http.Error(w, "Parse form error", http.StatusInternalServerError)
        return
    }

    email := r.FormValue("email")
    password := r.FormValue("password")

    var exists bool
    err = db.QueryRow("SELECT EXISTS (SELECT 1 FROM users WHERE email=$1)", email).Scan(&exists)
    if err != nil {
        http.Error(w, "Server error", http.StatusInternalServerError)
        return
    }
    if exists {
        http.Error(w, "Email already exists", http.StatusConflict)
        return
    }

    hashedPassword, err := hashPassword(password)
    if err != nil {
        http.Error(w, "Server error", http.StatusInternalServerError)
        return
    }

    var id int
    err = db.QueryRow("INSERT INTO users (email, password) VALUES ($1, $2) RETURNING id", email, hashedPassword).Scan(&id)
    if err != nil {
        http.Error(w, "Server error", http.StatusInternalServerError)
        return
    }

    token, err := generateJWT(id)
    if err != nil {
        http.Error(w, "Server error", http.StatusInternalServerError)
        return
    }

    http.SetCookie(w, &http.Cookie{
        Name:  "userID",
        Value: fmt.Sprintf("%d", id),
        Path:  "/",
    })
    http.SetCookie(w, &http.Cookie{
        Name:  "userEmail",
        Value: email,
        Path:  "/",
    })
    http.SetCookie(w, &http.Cookie{
        Name:  "token",
        Value: token,
        Path:  "/",
    })

    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, `{"userID": %d}`, id)
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
        return
    }

    err := r.ParseForm()
    if err != nil {
        http.Error(w, "Parse form error", http.StatusInternalServerError)
        return
    }

    email := r.FormValue("email")
    password := r.FormValue("password")

    var userID int
    var dbPassword string
    err = db.QueryRow("SELECT id, password FROM users WHERE email=$1", email).Scan(&userID, &dbPassword)
    if err != nil {
        if err == sql.ErrNoRows {
            http.Error(w, "Invalid email or password", http.StatusUnauthorized)
        } else {
            http.Error(w, "Server error", http.StatusInternalServerError)
        }
        return
    }

    if !checkPasswordHash(password, dbPassword) {
        http.Error(w, "Invalid email or password", http.StatusUnauthorized)
        return
    }

    token, err := generateJWT(userID)
    if err != nil {
        http.Error(w, "Server error", http.StatusInternalServerError)
        return
    }

    http.SetCookie(w, &http.Cookie{
        Name:  "userID",
        Value: fmt.Sprintf("%d", userID),
        Path:  "/",
    })
    http.SetCookie(w, &http.Cookie{
        Name:  "userEmail",
        Value: email,
        Path:  "/",
    })
    http.SetCookie(w, &http.Cookie{
        Name:  "token",
        Value: token,
        Path:  "/",
    })

    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintf(w, `{"userID": %d}`, userID)
}
