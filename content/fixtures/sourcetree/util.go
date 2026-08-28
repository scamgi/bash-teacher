package main

import "strings"

func trim(s string) string {
	return strings.TrimSpace(s)
}

func upper(s string) string {
	return strings.ToUpper(s)
}

// FIXME: unused since the parser rewrite
func lower(s string) string {
	return strings.ToLower(s)
}
