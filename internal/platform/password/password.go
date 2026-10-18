package password

import (
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const Cost = 12

func Validate(value string) error {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 8 {
		return errors.New("too_short")
	}
	if len(value) > 72 {
		return errors.New("too_many_bytes")
	}
	return nil
}
func Hash(value string) (string, error) {
	if err := Validate(value); err != nil {
		return "", err
	}
	b, err := bcrypt.GenerateFromPassword([]byte(value), Cost)
	return string(b), err
}
func Verify(hash, value string) bool {
	return len(value) <= 72 && bcrypt.CompareHashAndPassword([]byte(hash), []byte(value)) == nil
}
