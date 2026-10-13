package setup

import (
	"fmt"
	"strings"
)

// SlackManifest returns a Slack app manifest with every scope and event
// intern uses. Paste it at https://api.slack.com/apps > Create New App >
// From an app manifest. publicURL is where intern is reachable. Slack
// verifies the events URL when the manifest is saved, so without one the
// manifest leaves events out; once intern is deployed, paste the manifest
// with the URL into the app's App Manifest page.
func SlackManifest(publicURL string) string {
	events := ""
	if publicURL != "" {
		events = fmt.Sprintf(`  event_subscriptions:
    request_url: %s/slack/events
    bot_events:
      - app_mention
      - message.channels
      - message.groups
      - message.im
`, strings.TrimRight(publicURL, "/"))
	}
	return fmt.Sprintf(`display_information:
  name: AI Intern
  description: Plans, builds, reviews and tests code from Slack and JIRA.
  background_color: "#2c2d30"
features:
  bot_user:
    display_name: intern
    always_online: true
oauth_config:
  scopes:
    bot:
      - app_mentions:read
      - chat:write
      - channels:history
      - groups:history
      - im:history
      - im:read
settings:
%s  org_deploy_enabled: false
  socket_mode_enabled: false
  token_rotation_enabled: false
`, events)
}
