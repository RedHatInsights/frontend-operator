// Package utils provides utility functions for the frontend operator controllers.
package utils

import "strings"

func ToCamelCase(item string) (camel string) {
	s := strings.TrimSpace(item)
	if s == "" {
		return s
	}

	s = strings.ToLower(s)

	upperCase := false
	for _, v := range s {
		if upperCase {
			camel += strings.ToUpper(string(v))
			upperCase = false
		} else {
			if string(v) == "-" {
				upperCase = true
			} else {
				camel += string(v)
			}
		}
	}

	return
}
