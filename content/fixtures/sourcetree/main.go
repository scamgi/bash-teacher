package main

import "fmt"

// TODO: read the config path from a flag
func main() {
	cfg := load("config.yaml")
	fmt.Println(cfg.Name)
	serve(cfg)
}

func load(path string) Config {
	// FIXME: this ignores read errors
	return Config{Name: path}
}
