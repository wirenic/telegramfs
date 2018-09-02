package main

type tgConfig struct {
	ListenAddr string `json:"listen_addr"` // The file server will listen on this TCP address.
	Phone      string `json:"phone"`       // Your phone number.
	Key        string `json:"key"`         // An encryption key (used by tdlib).
	APIId      int    `json:"api_id"`
	APIHash    string `json:"api_hash"`
}
