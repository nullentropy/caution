module screensaver

go 1.26

require github.com/nullentropy/caution v0.0.0

require (
	github.com/ebitengine/oto/v3 v3.4.1 // indirect
	github.com/ebitengine/purego v0.9.0 // indirect
	github.com/go-gl/gl v0.0.0-20260331235117-4566fea9a276 // indirect
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20260802143932-8fa725040a18 // indirect
	github.com/go-text/typesetting v0.3.4 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/rs/zerolog v1.35.1 // indirect
	golang.org/x/image v0.45.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

// The module is fetchable straight from the (private) repo once you set
//   go env -w GOPRIVATE=github.com/nullentropy/*
// and route github over SSH. Inside this repo, the example resolves against
// the checkout instead:
replace github.com/nullentropy/caution => ../..

tool github.com/nullentropy/caution/cmd/appbundle
