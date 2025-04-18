package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Invitation struct {
	ID          string    `json:"id"`
	PhoneNumber string    `json:"phone_number"`
	Message     string    `json:"message,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
	Response    string    `json:"response,omitempty"`
	RespondedAt time.Time `json:"responded_at,omitempty"`
}

var (
	invitations = make(map[string]Invitation)
	mu          sync.Mutex
)

type createInvitationRequest struct {
	PhoneNumber string `json:"phone_number"`
	Message     string `json:"message"`
	DurationMin int    `json:"duration_min"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	var req createInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.PhoneNumber == "" || req.Message == "" || req.DurationMin <= 0 {
		writeError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	exp := time.Now().Add(time.Duration(req.DurationMin) * time.Minute)
	inv := Invitation{
		ID:          generateID(),
		PhoneNumber: req.PhoneNumber,
		Message:     req.Message,
		ExpiresAt:   exp,
		CreatedAt:   time.Now().UTC(),
	}

	mu.Lock()
	invitations[inv.ID] = inv
	mu.Unlock()

	sendSMS(inv.PhoneNumber, inv.Message, inv.ExpiresAt)
	writeJSON(w, http.StatusCreated, inv)
}

func handleRespondInvitation(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/invitations/")
	id = strings.TrimSuffix(id, "/respond")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing invitation ID")
		return
	}

	var req struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	resp := strings.ToLower(strings.TrimSpace(req.Response))
	if resp != "yes" && resp != "no" {
		writeError(w, http.StatusBadRequest, "response must be 'yes' or 'no'")
		return
	}

	mu.Lock()
	defer mu.Unlock()
	inv, ok := invitations[id]
	if !ok {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}
	if time.Now().After(inv.ExpiresAt) {
		writeError(w, http.StatusGone, "invitation has expired")
		sendSMS(inv.PhoneNumber, "Sorry, your invitation has expired.", time.Time{})
		return
	}
	if inv.Response != "" {
		writeError(w, http.StatusConflict, "invitation already responded to")
		return
	}

	inv.Response = resp
	inv.RespondedAt = time.Now().UTC()
	invitations[id] = inv

	sendSMS(inv.PhoneNumber, "Thanks! Your response has been recorded as: "+strings.Title(resp), time.Time{})
	writeJSON(w, http.StatusOK, map[string]string{"status": "response recorded"})
}

func sendSMS(phone, message string, expiresAt time.Time) {
	fullMessage := strings.TrimSpace(message)
	if !expiresAt.IsZero() {
		fullMessage += " This invitation will be open until " + expiresAt.Local().Format("3:04PM") + "."
	}
	log.Printf("📲 Sending SMS to %s: %s", phone, fullMessage)
}

func generateID() string {
	return time.Now().Format("20060102150405.000")
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /invitations", handleCreateInvitation)
	mux.HandleFunc("POST /invitations/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/respond") {
			handleRespondInvitation(w, r)
			return
		}
		writeError(w, http.StatusNotFound, "not found")
	})

	log.Println("🚀 API listening on :8080")
	http.ListenAndServe(":8080", mux)
}