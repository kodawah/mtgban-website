package main

import (
	"context"
	"encoding/csv"
	"flag"
	"io"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
)

func main() {
	filePathOpt := flag.String("path", "", "path")
	keyOpt := flag.String("key", "", "date")
	dbOpt := flag.Int("db", -1, "db#")
	modeOpt := flag.String("mode", "", "inv/bl")

	flag.Parse()

	if *keyOpt == "" || *filePathOpt == "" || *dbOpt < 0 || *dbOpt > 15 || *modeOpt == "" {
		log.Fatalln("missing key")
	}

	db := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   *dbOpt,
	})
	grade := map[string]float64{
		"NM": 1, "SP": 1.25, "MP": 1.67, "HP": 2.5, "PO": 4,
	}

	file, err := os.Open(*filePathOpt)
	if err != nil {
		log.Fatalln(err)

	}
	defer file.Close()

	csvReader := csv.NewReader(file)

	_, err = csvReader.Read()
	if err == io.EOF {
		log.Fatalln("empty")
	}

	start := time.Now()

	alreadyAdded := map[string]bool{}

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("error reading record: %v", err)
		}

		cardId := record[0]
		conditions := record[6]
		priceStr := record[7]

		if *modeOpt == "bl" && alreadyAdded[cardId] {
			continue
		}

		price, err := strconv.ParseFloat(priceStr, 64)
		if err != nil || price == 0 {
			log.Fatalf(cardId, "missing price:", err)
		}
		price = price * grade[conditions]

		err = db.HSet(context.Background(), cardId, *keyOpt, price).Err()
		if err != nil {
			log.Fatalf("redis error for %s: %s", cardId, err)
		}

		alreadyAdded[cardId] = true
	}

	log.Println("Took", time.Now().Sub(start))
}
