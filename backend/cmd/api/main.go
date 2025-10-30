package main

import (
	"context"
	"log"
	"os"

	"brokerflow/agreement"
	"brokerflow/db"
)

func main() {
	ctx := context.Background()

	connString := os.Getenv("DATABASE_URL")
	pool, err := db.NewPool(ctx, connString)
	if err != nil {
		log.Fatalf("bootstrap database pool: %v", err)
	}
	defer pool.Close()

	agreementService := agreement.NewService(pool, nil)

	log.Printf("agreement service ready: %+v", agreementService != nil)
}
