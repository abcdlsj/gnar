package main

import (
	"log"

	"github.com/abcdlsj/gnar/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		log.Fatal(err)
	}
}
