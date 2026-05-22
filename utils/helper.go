package utils

import "fmt"

// Greet returns a greeting for the given name.
func Greet(name string) string {
	if name == "" {
		return "Hello, stranger!"
	}
	return fmt.Sprintf("Hello, %s!", name)
}

// Add returns the sum of two integers.
func Add(a, b int) int {
	return a + b
}
