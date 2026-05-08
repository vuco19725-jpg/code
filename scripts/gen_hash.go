//go:build ignore
// +build ignore

package main

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	hash, _ := bcrypt.GenerateFromPassword([]byte("test123456"), bcrypt.DefaultCost)
	fmt.Println(string(hash))
}