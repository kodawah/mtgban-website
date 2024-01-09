package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	"firebase.google.com/go/auth"
	"google.golang.org/api/option"
)

var (
	ctx        context.Context
	authClient *auth.Client
	fsClient   *firestore.Client
)

func handleRequest(w http.ResponseWriter, r *http.Request) {
	idToken := r.FormValue("idToken")
	init_firebase()
	user, err := authenticateUser(ctx, authClient, idToken)
	if err != nil {
		log.Fatalf("authenticateUser: %v", err)
	}
	docRef := fsClient.Collection("users").Doc(user.UID)
	_, err = docRef.Set(ctx, map[string]interface{}{
		"name":  user.DisplayName,
		"email": user.Email,
	})
	if err != nil {
		log.Fatalf("docRef.Set: %v", err)
	}

	// Add user preferences to the Firestore document
	_, err = docRef.Update(ctx, []firestore.Update{
		{
			Path:  "preference",
			Value: "Preference value",
		},
	})
	if err != nil {
		log.Fatalf("docRef.Update: %v", err)
	}
}

func init_firebase() {
	// Initialize Firebase Auth and Firestore clients
	ctx = context.Background()
	initFirebaseClients(ctx)

	// Create an Auth client
	sa := option.WithCredentialsFile("path/to/your/serviceAccountKey.json")
	app, err := firebase.NewApp(ctx, nil, sa)
	if err != nil {
		log.Fatalf("firebase.NewApp: %v", err)
	}
	authClient, err = app.Auth(ctx)
	if err != nil {
		log.Fatalf("app.Auth: %v", err)
	}

	// Create a Firestore client
	fsClient, err = app.Firestore(ctx)
	if err != nil {
		log.Fatalf("app.Firestore: %v", err)
	}

	// User signs up or logs in
	user, err := authClient.GetUser(ctx, "some-uid")
	if err != nil {
		log.Fatalf("client.GetUser: %v", err)
	}

	// Create a user document with the unique user ID
	docRef := fsClient.Collection("users").Doc(user.UID)
	_, err = docRef.Set(ctx, map[string]interface{}{
		"name":  user.DisplayName,
		"email": user.Email,
	})
	if err != nil {
		log.Fatalf("docRef.Set: %v", err)
	}

	// Add user preferences to the Firestore document
	_, err = docRef.Update(ctx, []firestore.Update{
		{
			Path:  "preference",
			Value: "Preference value",
		},
	})
	if err != nil {
		log.Fatalf("docRef.Update: %v", err)
	}

	fmt.Printf("Successfully added preferences for user %s\n", user.UID)
}

func initFirebaseClients(ctx context.Context) {
	// Get the Firebase credentials from the environment
	opt := option.WithCredentialsFile(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))

	// Initialize the Firebase app
	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		log.Fatalf("error initializing app: %v\n", err)
	}

	// Initialize the Firebase Auth client
	authClient, err = app.Auth(ctx)
	if err != nil {
		log.Fatalf("error initializing auth client: %v\n", err)
	}

	// Create a client to the Firebase Firestore system
	firestoreClient, err := app.Firestore(ctx)
	if err != nil {
		log.Fatalf("error initializing firestore client: %v\n", err)
	}
	defer firestoreClient.Close()

	// Assign the initialized clients to the global variables
	ctx = ctx
	authClient = authClient
	fsClient = firestoreClient
}

func authenticateUser(ctx context.Context, client *auth.Client, idToken string) (*auth.UserRecord, error) {
	user, err := client.VerifyIDToken(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("error verifying ID token: %v\n", err)
	}

	displayName, _ := user.Claims["displayName"].(string)
	email, _ := user.Claims["email"].(string)

	return &auth.UserRecord{
		UserInfo: &auth.UserInfo{
			UID:           user.UID,
			DisplayName:   displayName,
			Email:         email,
		},
	}, nil
}
