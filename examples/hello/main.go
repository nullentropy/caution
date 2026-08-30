package main

import (
	"flag"
	"fmt"
	"log"

	caution "github.com/nullentropy/caution/go"
	"github.com/nullentropy/caution/go/native"
)

func mount(s *caution.Session) *caution.Node {
	count := 0

	counter := caution.Label("0").FontSize(44).Weight(600).
		Anchor(caution.A{Left: caution.Px(28), Top: caution.Px(64)})
	mirror := caution.Label("type below and press Enter").FontSize(13).Color("$inkFaint").
		Anchor(caution.A{Left: caution.Px(28), Top: caution.Px(190), Right: caution.Px(28)})

	bump := func(delta int) func() {
		return func() {
			count += delta
			counter.SetText(fmt.Sprint(count))
		}
	}

	s.SetMenu(caution.Menu{Title: "Counter", Items: []caution.MenuItem{
		{Title: "Increment", Key: "i", OnPick: bump(1)},
		{Title: "Decrement", Key: "shift+cmd+i", OnPick: bump(-1)},
		{Sep: true},
		{Title: "Reset", OnPick: func() {
			count = 0
			counter.SetText("0")
		}},
	}})

	window := caution.Panel().Bg("$panel").Radius(14).Border("$edge", 1).
		Shadow(36, 0, 16, "#00000080").
		W(460).H(300).
		Anchor(caution.A{CenterX: caution.Px(0), CenterY: caution.Px(-20)}).
		Kids(
			caution.Label("Hello from your own module").FontSize(17).Weight(600).
				Anchor(caution.A{Left: caution.Px(28), Top: caution.Px(24)}),
			counter,
			caution.HStack().Gap(8).
				Anchor(caution.A{Left: caution.Px(28), Top: caution.Px(128)}).
				Kids(
					caution.Button("−").OnClick(bump(-1)),
					caution.Button("+").Primary().OnClick(bump(1)),
				),
			caution.TextField("").Placeholder("say something…").
				Anchor(caution.A{Left: caution.Px(28), Bottom: caution.Px(28), Right: caution.Px(28)}).
				OnCommit(func(v string) {
					mirror.SetText(fmt.Sprintf("the server heard: %q", v))
				}),
			mirror,
		)

	return caution.Panel().Kids(
		window,
		caution.Label("served by the caution SDK - browser, desktop window, or unix socket, same binary").
			FontSize(12).Color("$inkFaint").
			Anchor(caution.A{CenterX: caution.Px(0), Bottom: caution.Px(18)}),
	)
}

func main() {
	nativeFlag := flag.Bool("native", false, "open as a desktop window instead of serving HTTP")
	addr := flag.String("addr", ":8080", `listen address - ":8080" or "unix:/path.sock"`)
	flag.Parse()

	caution.ServeClient() // the embedded terminal: host page + client runtime

	if *nativeFlag || native.InBundle() {
		if err := native.Run(mount, native.Options{Title: "Hello Caution", W: 720, H: 480}); err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Fatal(caution.Serve(*addr, mount))
}
