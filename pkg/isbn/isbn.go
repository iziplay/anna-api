package isbn

import (
	"strconv"
	"strings"
)

// To13 converts an ISBN-10 to ISBN-13 by prepending 978 and computing the check digit.
// Returns an empty string if the input is not a valid ISBN-10.
func To13(isbn10 string) string {
	if len(isbn10) != 10 {
		return ""
	}
	base := "978" + isbn10[:9]
	sum := 0
	for i, c := range base {
		d, err := strconv.Atoi(string(c))
		if err != nil {
			return ""
		}
		if i%2 == 0 {
			sum += d
		} else {
			sum += d * 3
		}
	}
	check := (10 - sum%10) % 10
	return base + strconv.Itoa(check)
}

// To10 converts a 978-prefixed ISBN-13 to ISBN-10.
// Returns an empty string if the input is not a convertible ISBN-13.
func To10(isbn13 string) string {
	if len(isbn13) != 13 || !strings.HasPrefix(isbn13, "978") {
		return ""
	}
	base := isbn13[3:12]
	sum := 0
	for i, c := range base {
		d, err := strconv.Atoi(string(c))
		if err != nil {
			return ""
		}
		sum += d * (10 - i)
	}
	check := (11 - sum%11) % 11
	if check == 10 {
		return base + "X"
	}
	return base + strconv.Itoa(check)
}
