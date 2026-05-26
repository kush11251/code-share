package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var tpl = template.Must(template.ParseFiles("templates/index.html"))
var redisClient *redis.Client
var hub *Hub

func main() {
	redisOptions := getRedisOptions()
	redisClient = redis.NewClient(redisOptions)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}

	hub = NewHub(redisClient)
	go hub.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)
	mux.HandleFunc("/ws/", wsHandler)
	mux.HandleFunc("/format", formatHandler)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))

	addr := getEnv("ADDR", "")
	if addr == "" {
		port := getEnv("PORT", "8080")
		addr = ":" + strings.TrimPrefix(port, ":")
	}
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getRedisOptions() *redis.Options {
	if url := os.Getenv("REDIS_URL"); url != "" {
		opts, err := redis.ParseURL(url)
		if err == nil {
			return opts
		}
	}

	host := os.Getenv("REDIS_HOST")
	port := os.Getenv("REDIS_PORT")
	password := os.Getenv("REDIS_PASSWORD")
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "6379"
	}

	return &redis.Options{
		Addr:     host + ":" + port,
		Password: password,
		DB:       0,
	}
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	if path == "" {
		id := generateRoomID()
		if err := createSession(r.Context(), id); err != nil {
			http.Error(w, "unable to create session", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/"+id, http.StatusFound)
		return
	}

	if !isValidRoomID(path) {
		http.NotFound(w, r)
		return
	}

	if err := createSessionIfMissing(r.Context(), path); err != nil {
		http.Error(w, "unable to initialize room", http.StatusInternalServerError)
		return
	}

	data := struct {
		RoomID string
	}{RoomID: path}

	if err := tpl.Execute(w, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func formatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Code     string `json:"code"`
		Language string `json:"language"`
	}

	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form payload", http.StatusBadRequest)
			return
		}
		payload.Code = r.FormValue("code")
		payload.Language = r.FormValue("language")
	}

	payload.Language = strings.ToLower(strings.TrimSpace(payload.Language))
	if strings.TrimSpace(payload.Code) == "" {
		respondJSONError(w, "Formatting failed: code is empty", http.StatusBadRequest)
		return
	}

	supported := map[string]bool{
		"javascript": true,
		"python":     true,
		"go":         true,
		"html":       true,
		"css":        true,
	}
	if !supported[payload.Language] {
		respondJSONError(w, "Formatting failed: unsupported language", http.StatusBadRequest)
		return
	}

	formatted, err := mockFormatCode(payload.Code, payload.Language)
	if err != nil {
		respondJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"code": formatted})
}

func respondJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func mockFormatCode(code, language string) (string, error) {
	if strings.Contains(code, "SYNTAX_ERROR") {
		return "", fmt.Errorf("Syntax error")
	}
	return strings.TrimRight(code, "\n") + "\n", nil
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	roomID := strings.TrimPrefix(r.URL.Path, "/ws/")
	roomID = strings.Trim(roomID, "/")
	if !isValidRoomID(roomID) {
		http.NotFound(w, r)
		return
	}

	hub.HandleWebSocket(w, r, roomID)
}

func generateRoomID() string {
	id := strings.ReplaceAll(uuid.New().String(), "-", "")
	return id[:6]
}

func isValidRoomID(id string) bool {
	if len(id) != 6 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func createSession(ctx context.Context, roomID string) error {
	item := map[string]interface{}{
		"created_at": time.Now().Unix(),
		"lock_holder": "",
		"code":        "",
	}
	key := sessionKey(roomID)
	if err := redisClient.HSet(ctx, key, item).Err(); err != nil {
		return err
	}
	return redisClient.Expire(ctx, key, 24*time.Hour).Err()
}

func createSessionIfMissing(ctx context.Context, roomID string) error {
	key := sessionKey(roomID)
	exists, err := redisClient.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if exists == 0 {
		return createSession(ctx, roomID)
	}
	return nil
}

func currentRoomCode(ctx context.Context, roomID string) (string, error) {
	code, err := redisClient.HGet(ctx, sessionKey(roomID), "code").Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return code, nil
}

func updateRoomCode(ctx context.Context, roomID, code string) error {
	pipe := redisClient.TxPipeline()
	pipe.HSet(ctx, sessionKey(roomID), "code", code)
	pipe.Expire(ctx, sessionKey(roomID), lockKeyTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func sessionKey(roomID string) string {
	return fmt.Sprintf("room:%s", roomID)
}
