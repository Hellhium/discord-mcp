package tools

// BotTools are the tools for principals with a bot credential.
func BotTools() []Tool {
	var out []Tool
	out = append(out, discoveryTools()...)
	out = append(out, messageTools()...)
	out = append(out, channelTools()...)
	return out
}

// EventTools are added for configured bots with events.
func EventTools() []Tool {
	var out []Tool
	return out
}

// WebhookTools are the only tools a webhook principal gets.
func WebhookTools() []Tool {
	var out []Tool
	return out
}
