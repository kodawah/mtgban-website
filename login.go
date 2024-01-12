package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"firebase.google.com/go/auth"
)

type LoginResponse struct {
	Message string `json:"message"`
}

func handleLogin(w http.ResponseWriter, r *http.Request, client *auth.Client) {
    ctx := r.Context()
    idToken := r.FormValue("idToken")

    userRecord, err := authenticateUser(ctx, client, idToken)
    if err != nil {
        http.Error(w, fmt.Sprintf("Authentication error: %v", err), http.StatusUnauthorized)
        return
    }

    response := LoginResponse{
        Message: fmt.Sprintf("User ID: %s", userRecord.UID),
    }

    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(response); err != nil {
        log.Printf("Error encoding JSON: %v", err)
        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
        return
    }
}