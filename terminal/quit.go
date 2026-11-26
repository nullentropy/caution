package terminal

// linkd is the App Intents daemon, and its quit requests would otherwise end the app
func quitAllowed(sender string) bool {
	return sender != "linkd"
}
