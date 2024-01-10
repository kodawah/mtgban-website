package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"

	"google.golang.org/api/secretmanager/v1"
)

var SecretsClient *secretmanager.Service

func initSecretClient(ctx context.Context, projectId string) error {
	var err error
	SecretsClient, err = secretmanager.NewService(ctx)
	if err != nil {
		return fmt.Errorf("failed to create secretmanager client: %v", err)
	}
	return nil
}

func accessSecret(ctx context.Context, projectID, secretID string) ([]byte, error) {
	accessReq := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretID)
	log.Printf("Access Request for Secret: %s", accessReq)

	result, err := SecretsClient.Projects.Secrets.Versions.Access(accessReq).Context(ctx).Do()
	if err != nil {
		log.Printf("failed to access secret: %v", err)
		return nil, err
	}

	decodedData, err := base64.StdEncoding.DecodeString(result.Payload.Data)
	if err != nil {
		return nil, err
	}
	return decodedData, nil
}

func updateSecret(ctx context.Context, projectID, secretID, payload string) error {
	parent := fmt.Sprintf("projects/%s/secrets/%s", projectID, secretID)
	log.Printf("Parent ID for adding new secret version: %s", parent)

	encodedPayload := base64.StdEncoding.EncodeToString([]byte(payload))

	secretPayload := &secretmanager.AddSecretVersionRequest{
		Payload: &secretmanager.SecretPayload{
			Data: encodedPayload,
		},
	}

	_, err := SecretsClient.Projects.Secrets.AddVersion(parent, secretPayload).Context(ctx).Do()
	if err != nil {
		log.Printf("failed to add secret version: %v", err)
		return err
	}

	log.Printf("new version of %s added", secretID)
	return nil
}