package main

import (
	"flag"
	"log"

	"github.com/nullentropy/caution/internal/notices"
)

func main() {
	out := flag.String("o", "THIRD_PARTY_NOTICES.txt", "file to write")
	flag.Parse()
	builds, err := notices.Repo(".")
	if err != nil {
		log.Fatal(err)
	}
	if err := notices.WriteFile(*out, builds...); err != nil {
		log.Fatal(err)
	}
}
