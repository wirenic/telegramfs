package main

import (
	"time"
)

// A basic representation of a message. Telegram messages are much richer.
// We save these to our local database.
type tgMessage struct {
	ID         int64
	ChatID     int64
	When       time.Time
	Sender     string
	QuotedText string
	Text       string
}
