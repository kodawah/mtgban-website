package main

import (
	"context"
	"fmt"
	"net/http"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"firebase.google.com/go/auth"
	"google.golang.org/api/option"
)


func handleRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, authClient *auth.Client, fsClient *firestore.Client) {
	idToken := r.FormValue("idToken")
	user, err := authenticateUser(ctx, authClient, idToken)
	if err != nil {
		http.Error(w, fmt.Sprintf("Authentication error: %v", err), http.StatusInternalServerError)
		return
	}


	docRef := fsClient.Collection("users").Doc(user.UID)
	_, err = docRef.Set(ctx, map[string]interface{}{
		"name":  user.DisplayName,
		"email": user.Email,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to set document: %v", err), http.StatusInternalServerError)
        return
	}
	// this can be combined with call above for less latency (just fyi)
	_, err = docRef.Update(ctx, []firestore.Update{
		{
			Path:  "preference", 
			Value: "Preference value",
		},
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to update document: %v", err), http.StatusInternalServerError)
        return
	}
}

func initFirebase(ctx context.Context, projectId, secretId string) (*auth.Client, *firestore.Client, error) {
	err  := initSecretClient(ctx, projectId)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize secret client: %v", err)
	}
	serviceAccountJSON, err := accessSecret(ctx, projectId, secretId)
	if err != nil {
        return nil, nil, fmt.Errorf("failed to access service account JSON: %v", err)
    }

	opt := option.WithCredentialsJSON(serviceAccountJSON)
	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		return nil, nil, fmt.Errorf("firebase.NewApp: %v", err)
	}

	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("app.Auth: %v", err)
	}

	fsClient, err := app.Firestore(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("app.Firestore: %v", err)
	}

	return authClient, fsClient, nil
}

func authenticateUser(ctx context.Context, client *auth.Client, idToken string) (*auth.UserRecord, error) {
	token, err := client.VerifyIDToken(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("error verifying ID token: %v\n", err)
	}

	
	return &auth.UserRecord{
		UserInfo: &auth.UserInfo{
			UID:           token.UID,
			DisplayName:   token.Claims["name"].(string),
			Email:         token.Claims["email"].(string),
		},
	}, nil
}
