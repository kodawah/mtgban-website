package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

func main() {
	os.Exit(run())
}

func run() int {
	credentialsPathOpt := flag.String("credentials", "credentials.json", "Load credentials file")
	projectOpt := flag.String("project", "", "Project id")
	queryOpt := flag.String("query", "", "Query to run")

	flag.Parse()

	if *credentialsPathOpt == "" || *projectOpt == "" || *queryOpt == "" {
		fmt.Fprintln(os.Stderr, "Missing flag, see -h")
		return 1
	}

	// Set up a context and a BigQuery client.
	ctx := context.Background()
	client, err := bigquery.NewClient(ctx, *projectOpt, option.WithCredentialsFile(*credentialsPathOpt))
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}

	// Create a query and read from it
	query := client.Query(*queryOpt)
	it, err := query.Read(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}

	// Print the rows.
	var row []bigquery.Value
	for {
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return 1
		}
		fmt.Println(row)
	}

	return 0
}
