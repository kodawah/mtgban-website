package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/iterator"
)

func updateSecretFromFile(ctx context.Context, client *secretmanager.Client, projectID, secretID, filePath string) string {
	// Read the file contents
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatalf("failed to read file: %v", err)
	}

	// Build the request
	addSecretVersionReq := &secretmanagerpb.AddSecretVersionRequest{
		Parent: fmt.Sprintf("projects/%s/secrets/%s", projectID, secretID),
		Payload: &secretmanagerpb.SecretPayload{
			Data: fileData,
		},
	}

	// Call the API
	version, err := client.AddSecretVersion(ctx, addSecretVersionReq)
	if err != nil {
		log.Fatalf("failed to add secret version: %v", err)
	}

	fmt.Printf("Added secret version: %s\n", version.Name)
	return version.Name
}

func disablePreviousVersion(ctx context.Context, client *secretmanager.Client, parent string, currentVersion string) {
	// List all secret versions
	req := &secretmanagerpb.ListSecretVersionsRequest{Parent: parent}
	it := client.ListSecretVersions(ctx, req)
	var previousVersion string

	for {
		version, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("failed to list secret versions: %v", err)
		}

		// Skip the current version
		if version.Name == currentVersion {
			continue
		}

		previousVersion = version.Name
	}

	if previousVersion != "" {
		// Disable the previous version
		disableReq := &secretmanagerpb.DisableSecretVersionRequest{Name: previousVersion}
		_, err := client.DisableSecretVersion(ctx, disableReq)
		if err != nil {
			log.Fatalf("failed to disable secret version: %v", err)
		}
		fmt.Printf("Disabled previous secret version: %s\n", previousVersion)
	}
}

func main() {
	projectID := flag.String("project", "ban-on-fire", "Google Cloud project ID")
	secretID := flag.String("name", "", "Resource ID of the secret")
	filePath := flag.String("file", "", "Path to the file containing the secret data")
	disablePrev := flag.Bool("disable", false, "Disable the previous secret version")

	flag.Parse()

	if *secretID == "" || *filePath == "" {
		fmt.Println("Flags 'secret' and 'file' are required.")
		flag.Usage()
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		log.Fatalf("failed to create secretmanager client: %v", err)
	}
	defer client.Close()

	parent := fmt.Sprintf("projects/%s/secrets/%s", *projectID, *secretID)
	currentVersion := updateSecretFromFile(ctx, client, *projectID, *secretID, *filePath)

	if *disablePrev {
		disablePreviousVersion(ctx, client, parent, currentVersion)
	}
}
