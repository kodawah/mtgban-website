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
	datasetOpt := flag.String("dataset", "", "Dataset id")
	tableOpt := flag.String("table", "", "Table")

	flag.Parse()

	if *credentialsPathOpt == "" || *projectOpt == "" || *datasetOpt == "" || *tableOpt == "" {
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

	// Set the ID of the dataset and the name of the table or view to read.
	datasetID := *datasetOpt
	tableID := *tableOpt

	// Create a reference to the table or view.
	table := client.Dataset(datasetID).Table(tableID)

	// Read the rows from the table or view.
	it := table.Read(ctx)

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
