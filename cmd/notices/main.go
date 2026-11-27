package main

import (
	"flag"
	"log"

	"github.com/nullentropy/caution/internal/notices"
)

func main() {
	out := flag.String("o", "", "file to write (default THIRD_PARTY_NOTICES.txt, required with -wasm)")
	wasm := flag.Bool("wasm", false, "cover only the browser terminal, embedded/term.wasm.gz")
	flag.Parse()
	if *out == "" {
		if *wasm {
			log.Fatal("notices: -wasm needs -o, so it can't replace the repository's THIRD_PARTY_NOTICES.txt")
		}
		*out = "THIRD_PARTY_NOTICES.txt"
	}
	var builds []*notices.Build
	var err error
	if *wasm {
		var b *notices.Build
		b, err = notices.FromPackages(".", notices.Target{GOOS: "js", GOARCH: "wasm"}, "./cmd/termwasm")
		builds = []*notices.Build{b}
	} else {
		builds, err = notices.Repo(".")
	}
	if err != nil {
		log.Fatal(err)
	}
	if err := notices.WriteFile(*out, builds...); err != nil {
		log.Fatal(err)
	}
}
