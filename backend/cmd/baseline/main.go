package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/bogdandobrica/modelsays/backend/internal/baseline"
)

func main() {
	databaseURL := flag.String("database-url", "", "PostgreSQL URL used to create isolated workload schemas")
	clientDist := flag.String("client-dist", "../client/dist", "path to the built client distribution")
	flag.Parse()

	report, err := baseline.Run(context.Background(), baseline.Options{
		DatabaseURL: *databaseURL,
		ClientDist:  *clientDist,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "baseline failed: %v\n", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "encode baseline report: %v\n", err)
		os.Exit(1)
	}
}
