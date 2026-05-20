package plexdash

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// PublishMQTT connects to the broker, publishes payload as JSON, and disconnects.
// Returns nil if broker is empty (feature disabled).
func PublishMQTT(broker, topic, username, password string, payload any) error {
	if strings.TrimSpace(broker) == "" {
		return nil
	}
	if strings.TrimSpace(topic) == "" {
		topic = "plex/volume"
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mqtt marshal: %w", err)
	}

	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID("plex-dashboard").
		SetConnectTimeout(8 * time.Second).
		SetAutoReconnect(false)
	if username != "" {
		opts.SetUsername(username)
		opts.SetPassword(password)
	}

	c := mqtt.NewClient(opts)
	tok := c.Connect()
	if !tok.WaitTimeout(8 * time.Second) {
		return fmt.Errorf("mqtt connect timed out")
	}
	if err := tok.Error(); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	defer c.Disconnect(500)

	pub := c.Publish(topic, 0, false, data)
	if !pub.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt publish timed out")
	}
	return pub.Error()
}
