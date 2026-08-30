// caution preview: point it at a .ui.json document and open the browser
//
//	go run ./cmd/preview [-addr :9900] path/to/design.ui.json
package main

import (
	"flag"
	"log"
	"os"
	"time"

	"github.com/nullentropy/caution/go"
	"github.com/nullentropy/caution/go/dev"
)

func main() {
	addr := flag.String("addr", ":9900", "listen address")
	flag.Parse()
	path := flag.Arg(0)
	if path == "" {
		log.Fatal("usage: preview [-addr :9900] <file.ui.json>")
	}
	dev.ServeClient("../src/terminal.ts")
	log.Fatal(caution.Serve(*addr, func(s *caution.Session) *caution.Node {
		go watch(s, path)
		return load(path)
	}))
}

func load(path string) *caution.Node {
	data, err := os.ReadFile(path)
	if err != nil {
		return errorTree(err)
	}
	ui, err := caution.LoadUI(data)
	if err != nil {
		return errorTree(err)
	}
	return ui.Root
}

func errorTree(err error) *caution.Node {
	return caution.Panel().Kids(
		caution.Label("caution preview").FontSize(15).Weight(600).
			Anchor(caution.A{Left: caution.Px(24), Top: caution.Px(24)}),
		caution.Label(err.Error()).FontSize(13).Mono().Color("#ff6b6b").Selectable().
			Anchor(caution.A{Left: caution.Px(24), Top: caution.Px(52), Right: caution.Px(24)}),
		caution.Label("fix the document and save - the preview reloads on its own").
			FontSize(13).Color("$inkDim").
			Anchor(caution.A{Left: caution.Px(24), Top: caution.Px(80)}),
	)
}

func watch(s *caution.Session, path string) {
	var last time.Time
	if fi, err := os.Stat(path); err == nil {
		last = fi.ModTime()
	}
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-s.Done():
			return
		case <-t.C:
			fi, err := os.Stat(path)
			if err != nil || fi.ModTime().Equal(last) {
				continue
			}
			last = fi.ModTime()
			s.Update(func() { s.SetRoot(load(path)) })
		}
	}
}
