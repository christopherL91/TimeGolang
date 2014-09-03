package main

//Describes a websocket message.
type Message struct {
	To   []string               `json:"to"`
	Body map[string]interface{} `json:"body"`
}
