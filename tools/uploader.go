package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"path"
	"time"

	"cloud.google.com/go/storage"
	"github.com/mtgban/go-mtgban/mtgban"
	"google.golang.org/api/option"
)

const DefaultUploaderTimeout = 60 * time.Second

func run() error {
	svcAcc := flag.String("svc", "credentials.json", "Load service account file")
	filePath := flag.String("path", "", "Target file path")
	invFile := flag.String("file", "inventory.json", "Inventory file to upload")
	bucketName := flag.String("bucket", "", "The bucket where to upload")
	flag.Parse()

	if *bucketName == "" {
		return errors.New("missing bucket")
	}

	ctx := context.Background()
	GCSBucketClient, err := storage.NewClient(ctx, option.WithCredentialsFile(*svcAcc))
	if err != nil {
		return err
	}

	file, err := os.Open(*invFile)
	if err != nil {
		return err
	}
	defer file.Close()

	seller, err := mtgban.ReadSellerFromJSON(file)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultUploaderTimeout)
	defer cancel()

	outputPath := *filePath + path.Base(*invFile)
	log.Println("Uploading to", outputPath)
	wc := GCSBucketClient.Bucket(*bucketName).Object(outputPath).NewWriter(ctx)
	wc.ContentType = "application/json"
	defer wc.Close()

	return mtgban.WriteSellerToJSON(seller, wc)
}

func main() {
	err := run()
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
