package main

import (
	"log"

	"pictide.local/studio/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
