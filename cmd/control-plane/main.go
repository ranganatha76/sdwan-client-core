package main

import "log"

// main constructs the control-plane server and starts its HTTP listener.
func main() {
	server := newServer()
	log.Println("SD-WAN client listening on http://localhost:8080")
	log.Fatal(server.listenAndServe())
}
