package terminal

// stop linkd (App Intents) from killing us for no good reason
func quitAllowed(sender string) bool {
	return sender != "linkd"
}
