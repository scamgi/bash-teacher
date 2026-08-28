package main

import "net/http"

type Config struct {
	Name string
	Port int
}

// serve starts the listener.
func serve(c Config) {
	http.HandleFunc("/", handle)
	// TODO: honour c.Port
	_ = http.ListenAndServe(":8080", nil)
}

func handle(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("ok"))
}
