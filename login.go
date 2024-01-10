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

func loginHandler(w http.ResponseWriter, r *http.Request, client *auth.Client) {
	ctx := r.Context()
	idToken := r.Header.Get("Authorization")
	if idToken == "" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	token, err := client.VerifyIDToken(ctx, idToken)
	if err != nil {
		log.Printf("Token verification failed: %v", err)
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	uid, ok := token.Claims["user_id"].(string)
	if !ok {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	response := LoginResponse{
		Message: fmt.Sprintf("User ID: %s", uid),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding JSON: %v", err)
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
}
