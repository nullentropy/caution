module screensaver

go 1.26

require github.com/nullentropy/caution/go v0.0.0

require (
	github.com/go-gl/gl v0.0.0-20260331235117-4566fea9a276 // indirect
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20260802143932-8fa725040a18 // indirect
	github.com/go-text/typesetting v0.3.4 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	golang.org/x/image v0.45.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

// The module is fetchable straight from the (private) repo once you set
//   go env -w GOPRIVATE=github.com/nullentropy/*
// and route github over SSH. Inside this repo, the example resolves against
// the checkout instead:
replace github.com/nullentropy/caution/go => ../../go

tool github.com/nullentropy/caution/go/cmd/appbundle
