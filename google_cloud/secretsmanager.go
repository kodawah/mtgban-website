package google_cloud

import (
	"context"
	"encoding/json"
	"fmt"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"golang.org/x/oauth2/google"
)

// SecretInfo represents necessary details (name and version) to retrieve a specific secret.
type SecretInfo struct {
	Name string `json:"name"`
	Ver  int    `json:"ver"`
}

// ServiceAccountCredentials defines the structure for service accounnt credentials, duh.
type ServiceAccountCredentials struct {
	Type                    string `json:"type"`
	ProjectID               string `json:"project_id"`
	PrivateKeyID            string `json:"private_key_id"`
	PrivateKey              string `json:"private_key"`
	ClientEmail             string `json:"client_email"`
	ClientID                string `json:"client_id"`
	AuthURI                 string `json:"auth_uri"`
	TokenURI                string `json:"token_uri"`
	AuthProviderX509CertURL string `json:"auth_provider_x509_cert_url"`
	ClientX509CertURL       string `json:"client_x509_cert_url"`
}

// RetrieveSecretAsString fetches the secret value as a raw string from Google Secret Manager.
// For secrets that are simple strings, such as API keys or database passwords.
func RetrieveSecretAsString(ctx context.Context, secretResourceID string) (string, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create secret manager client: %v", err)
	}
	defer client.Close()

	request := &secretmanagerpb.AccessSecretVersionRequest{Name: secretResourceID}
	result, err := client.AccessSecretVersion(ctx, request)
	if err != nil {
		return "", fmt.Errorf("failed to access secret version: %v", err)
	}
	secretValue := string(result.Payload.Data)
	return secretValue, nil
}

// FetchServiceAccountCredentials specifically is used for retrieving and unmarshalling service account credentials structured in JSON format.
func FetchServiceAccountCredentials(ctx context.Context, secretResourceID string) (*ServiceAccountCredentials, error) {
	secretValue, err := RetrieveSecretAsString(ctx, secretResourceID)
	if err != nil {
		return nil, err
	}

	var credentials ServiceAccountCredentials
	if err := json.Unmarshal([]byte(secretValue), &credentials); err != nil {
		return nil, fmt.Errorf("failed to unmarshal service account credentials: %w", err)
	}
	return &credentials, nil
}

// GetServiceAccountCredentials creates Google credentials from a service account configuration.
// Used to authenticate a client for various Google Cloud services.
func GetServiceAccountCredentials(ctx context.Context, projectID string, serviceAccount SecretInfo) (*google.Credentials, error) {
	secretResourceID := fmt.Sprintf("projects/%s/secrets/%s/versions/%d", projectID, serviceAccount.Name, serviceAccount.Ver)
	credentials, err := FetchServiceAccountCredentials(ctx, secretResourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch service account credentials: %v", err)
	}

	jsonCredentials, err := json.Marshal(credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal credentials to JSON: %v", err)
	}

	googleCredentials, err := google.CredentialsFromJSON(ctx, jsonCredentials)
	if err != nil {
		return nil, fmt.Errorf("failed to create Google credentials from JSON: %v", err)
	}
	return googleCredentials, nil
}

// For use in main.go
// import {
//	"github.com/kodabb/mtgban-website/google_cloud/secretsmanager"
// }
// func main() {
//	ctx := context.Background()
//
//  Load Config struct from config.json.
//
// Example usage for retrieving service account credentials:
//
//firebaseSignerConfig := Config.SA.Firebase
//creds, err := secretsmanager.GetServiceAccountCredentials(ctx, Config.ProjectId, secretsmanager.SecretInfo{
//    Name: urlSignerConfig.Name,
//    Ver:  urlSignerConfig.Ver,
//})
//if err != nil {
//    log.Fatalf("Failed to get service account credentials: %v", err)
//}
// Use the creds as needed for authenticating clients for various Google Cloud services.
//
// For non-service account secrets, you can retrieve them as raw strings:
// secretResourceID := fmt.Sprintf("projects/%s/secrets/%s/versions/%s", Config.ProjectId, "your-secret-name", "latest")
// rawSecretValue, err := secretsmanager.RetrieveSecretAsString(ctx, secretResourceID)
// if err != nil {
//     log.Fatalf("Failed to retrieve raw secret value: %v", err)
// }
// Use the rawSecretValue as needed in your application.
