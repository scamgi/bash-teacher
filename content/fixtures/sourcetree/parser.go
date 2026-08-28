package main

import "errors"

var errEmpty = errors.New("empty input")

func parse(s string) ([]string, error) {
	if s == "" {
		return nil, errEmpty
	}
	// TODO: handle quoted fields
	return split(s), nil
}

func split(s string) []string {
	return []string{s}
}
