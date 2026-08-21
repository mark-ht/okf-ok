package main

import (
	"context"
	"os"

	"github.com/mark-ht/okf-ok/internal/lint"
)

func main() {
	os.Exit(lint.Main(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
