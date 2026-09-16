package tools

import "github.com/Hellhium/discord-mcp/internal/auth"

// BotTools are the tools for principals with a bot credential.
func BotTools() []Tool {
	var out []Tool
	out = append(out, discoveryTools()...)
	out = append(out, messageTools()...)
	out = append(out, channelTools()...)
	out = append(out, memberTools()...)
	out = append(out, requestTool())
	return out
}

// EventTools are added for configured bots with events.
func EventTools() []Tool {
	return []Tool{pollEventsTool(), waitForMessageTool()}
}

// WebhookTools are the only tools a webhook principal gets.
func WebhookTools() []Tool {
	return []Tool{webhookGetTool(), webhookSendTool(), webhookGetMessageTool(), webhookEditMessageTool(), webhookDeleteMessageTool()}
}

const baseInstructions = "You act as a Discord bot through this server. Discord's own permissions decide what succeeds, " +
	"and every call is recorded in an audit log. Pass Discord IDs as strings. " +
	"Start with discord_list_guilds and discord_list_channels. " +
	"Use discord_request for Discord API endpoints that have no dedicated tool."

// Instructions returns the MCP handshake instructions for a capability set.
// They are fixed text: no instance name, token, webhook or other config value.
func Instructions(c auth.Capability) string {
	switch c {
	case auth.CapBotEvents:
		return baseInstructions + " Live Discord events are buffered: use discord_wait_for_message to wait for replies " +
			"and discord_poll_events to catch up, passing back the next_cursor each call returns."
	case auth.CapWebhook:
		return "You act as one Discord webhook through this server: you can post messages as it and read, edit or delete " +
			"the messages it sent. Every call is recorded in an audit log. Pass Discord IDs as strings."
	default:
		return baseInstructions
	}
}
