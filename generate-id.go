package eurus

import "math/rand/v2"

var idChars = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_")

func generateID() string {
	b := make([]rune, 32)
	for i := range b {
		b[i] = idChars[rand.N(len(idChars))]
	}
	return string(b)
}
