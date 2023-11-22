package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"cloud.google.com/go/storage"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

type SecretsConfig struct {
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

func getSecret() (*SecretsConfig, error) {
	projectID := Config.ProjectId
	secretID := os.Getenv("SECRET_ID")

	ctx := context.Background()
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to setup client: %w", err)
	}

	secretName := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretID)
	request := &secretmanagerpb.AccessSecretVersionRequest{Name: secretName}

	result, err := client.AccessSecretVersion(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to access secret version: %w", err)
	}

	var config SecretsConfig
	if err := json.Unmarshal(result.Payload.Data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal secret to service account key: %w", err)
	}

	return &config, nil
}

func accessSecret() (*storage.Client, error) {
	config, err := getSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	jsonKey, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config to JSON: %w", err)
	}

	creds, err := google.CredentialsFromJSON(context.Background(), jsonKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create credentials from JSON: %w", err)
	}

	client, err := storage.NewClient(context.Background(), option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}

	return client, nil
}
