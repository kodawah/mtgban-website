package google_cloud

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	firebaseAuth "firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

type SubscriptionDetails struct {
	Tier   string `firestore:"tier"`
	Status string `firestore:"status"`
}

type Config struct {
	SA struct {
		Firebase string `json:"firebase"`
	} `json:"sa"`
}

// FirebaseInit initializes the Firebase app and returns the Auth and Firestore clients.
func FirebaseInit(ctx context.Context, opts ...option.ClientOption) (*firebaseAuth.Client, *firestore.Client, error) {
	log.Println("Initializing Firebase app")
	app, err := firebase.NewApp(ctx, nil, opts...)
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

func AuthenticateUser(ctx context.Context, client *firebaseAuth.Client, idToken string) (*firebaseAuth.UserRecord, error) {
	log.Printf("Authenticating user with token: %s\n", idToken)
	token, err := client.VerifyIDToken(ctx, idToken)
	if err != nil {
		log.Printf("Failed to verify ID token: %v\n", err)
		return nil, fmt.Errorf("error verifying ID token: %v", err)
	}
	log.Println("User authenticated successfully")
	return &firebaseAuth.UserRecord{
			UserInfo: &firebaseAuth.UserInfo{
				UID:         token.UID,
				DisplayName: token.Claims["name"].(string),
				Email:       token.Claims["email"].(string),
			},
		},
		nil
}

func GetUserSubscriptionDetails(ctx context.Context, fsClient *firestore.Client, userID string) (*SubscriptionDetails, error) {
	log.Printf("Retrieving subscription details for user: %s\n", userID)
	docRef := fsClient.Collection("subscriptions").Doc(userID)
	docSnapshot, err := docRef.Get(ctx)
	if err != nil {
		log.Printf("Failed to retrieve subscription details: %v\n", err)
		return nil, err
	}
	var details SubscriptionDetails
	if err := docSnapshot.DataTo(&details); err != nil {
		return nil, err
	}
	log.Println("Subscription details retrieved successfully")
	return &details, nil
}

func HandleRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, auth *firebaseAuth.Client, fsClient *firestore.Client) {
	idToken := r.FormValue("idToken")
	if idToken == "" {
		log.Println("No ID token provided in request")
		http.Error(w, "No ID token provided", http.StatusBadRequest)
		return
	}

	log.Printf("Received ID token for authentication: %s", idToken)
	user, err := AuthenticateUser(ctx, auth, idToken)
	if err != nil {
		log.Printf("Authentication error: %v", err)
		http.Error(w, fmt.Sprintf("Authentication error: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Authenticated user: %s", user.UID)
	docRef := fsClient.Collection("users").Doc(user.UID)
	docSnapshot, err := docRef.Get(ctx)
	if err != nil && !docSnapshot.Exists() {
		log.Printf("Creating new document for user: %s", user.UID)
		_, err = docRef.Set(ctx, map[string]interface{}{
			"name":       user.DisplayName,
			"email":      user.Email,
			"signUpDate": firestore.ServerTimestamp,
		})
		if err != nil {
			log.Printf("Failed to create new user document: %v", err)
			http.Error(w, fmt.Sprintf("Failed to create new user document: %v", err), http.StatusInternalServerError)
			return
		}
	} else if err == nil {
		log.Printf("Updating last login for user: %s", user.UID)
		_, err = docRef.Update(ctx, []firestore.Update{
			{Path: "lastLogin", Value: firestore.ServerTimestamp},
		})
		if err != nil {
			log.Printf("Failed to update last login: %v", err)
			http.Error(w, fmt.Sprintf("Failed to update last login: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		log.Printf("Error checking user document existence: %v", err)
		http.Error(w, fmt.Sprintf("Error checking user document: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully handled request for user: %s", user.UID)
	// Respond to the client
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "User %s processed successfully", user.UID)
}
