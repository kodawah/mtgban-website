package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
)

func main() {
	keyOpt := flag.String("key", "", "date")
	dbOpt := flag.Int("db", -1, "db#")

	flag.Parse()

	if *keyOpt == "" || *dbOpt < 0 || *dbOpt > 15 {
		log.Fatalln("missing key")
	}

	db := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   *dbOpt,
	})

	start := time.Now()

	var cursor uint64
	for {
		var keys []string
		var err error
		keys, cursor, err = db.Scan(context.Background(), cursor, "*", 10).Result()
		if err != nil {
			log.Println(err)
			break
		}

		for _, cardId := range keys {
			err = db.HDel(context.Background(), cardId, *keyOpt).Err()
			if err != nil {
				continue
			}
		}

		if cursor == 0 {
			break
		}
	}

	log.Println("Took", time.Now().Sub(start))
}
