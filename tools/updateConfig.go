package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"os"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

const DefaultUploaderTimeout = 60 * time.Second

type ScraperConfig struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand"`
	Path      string `json:"path"`
}

func downloadScrapersConfig(path string) (map[string]*ScraperConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultUploaderTimeout)
	defer cancel()

	rc, err := GCSBucketClient.Bucket(BucketName).Object(path).NewReader(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var config map[string]*ScraperConfig
	err = json.NewDecoder(rc).Decode(&config)
	return config, err
}

func uploadScrapersConfig(config map[string]*ScraperConfig, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultUploaderTimeout)
	defer cancel()

	wc := GCSBucketClient.Bucket(BucketName).Object(path).NewWriter(ctx)
	wc.ContentType = "application/json"
	defer wc.Close()

	return json.NewEncoder(wc).Encode(&config)
}

var GCSBucketClient *storage.Client
var BucketName string

type Arguments struct {
	serviceAccount string
	bucketName     string
	target         string
	key            string
	name           string
	path           string
	deleteEntry    bool
}

func update(args Arguments) error {
	ctx := context.Background()
	gcsClient, err := storage.NewClient(ctx, option.WithCredentialsFile(args.serviceAccount))
	if err != nil {
		return err
	}

	GCSBucketClient = gcsClient
	BucketName = args.bucketName

	config, err := downloadScrapersConfig(args.target)
	if err != nil {
		return err
	}

	if args.deleteEntry {
		delete(config, args.key)
	} else {
		_, found := config[args.key]
		if !found {
			if args.name == "" {
				return errors.New("missing argument")
			}
			config[args.key] = &ScraperConfig{
				Name:      args.name,
				Shorthand: args.key,
			}
		}
		config[args.key].Path = args.path
	}

	return uploadScrapersConfig(config, args.target)
}

func run() error {
	svcAcc := flag.String("svc", "credentials.json", "Load service account file")
	bucketName := flag.String("bucket", "", "The bucket where to upload")
	target := flag.String("target", "", "Which file to update")
	key := flag.String("key", "", "Which scraper to update")
	name := flag.String("name", "", "If new key, the name of the scraper")
	path := flag.String("path", "", "The new path")
	deleteEntry := flag.Bool("delete", false, "Delete the key")
	flag.Parse()

	if *bucketName == "" || *key == "" || (*path == "" && !*deleteEntry) {
		return errors.New("missing argument")
	}

	if *target != "sellers.json" && *target != "vendors.json" {
		return errors.New("invalid target")
	}

	if *deleteEntry {
		log.Printf("Deleting %s from gs://%s/%s", *key, *bucketName, *target)
	} else {
		log.Printf("Updating gs://%s/%s with %s -> %s", *bucketName, *target, *key, *path)
	}

	err := update(Arguments{
		serviceAccount: *svcAcc,
		bucketName:     *bucketName,
		target:         *target,
		key:            *key,
		name:           *name,
		deleteEntry:    *deleteEntry,
	})
	if err != nil {
		return err
	}

	log.Println("All done")

	return nil
}

func main() {
	err := run()
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
