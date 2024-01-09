package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"firebase.google.com/go/auth"
)

func loginHandler(w http.ResponseWriter, r *http.Request, client *auth.Client) {
	idToken := r.Header.Get("Authorization")
	if idToken == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	token, err := client.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	response := LoginResponse{
		Message: fmt.Sprintf("Hello, %s! This is a protected route.", token.UID),
	}

	json.NewEncoder(w).Encode(response)
}
