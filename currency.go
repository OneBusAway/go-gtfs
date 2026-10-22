package gtfs

import (
	"strconv"
	"strings"
)

// CurrencyAmount is an exact amount of money: Units of 10^-Scale of
// Currency. For example, 2.75 USD is {Units: 275, Scale: 2, Currency: "USD"}.
// It keeps the precision the feed gave, so 2.50 has Scale 2 and 2.5 has
// Scale 1, and never rounds the way a float64 would.
type CurrencyAmount struct {
	Units    int64
	Scale    int32
	Currency string
}

// String renders the amount as the feed wrote it, followed by the currency,
// such as "2.75 USD".
func (a CurrencyAmount) String() string {
	digits := strconv.FormatInt(a.Units, 10)
	sign := ""
	if a.Units < 0 {
		sign, digits = "-", digits[1:]
	}
	if a.Scale > 0 {
		if pad := int(a.Scale) + 1 - len(digits); pad > 0 {
			digits = strings.Repeat("0", pad) + digits
		}
		digits = digits[:len(digits)-int(a.Scale)] + "." + digits[len(digits)-int(a.Scale):]
	}
	if a.Currency == "" {
		return sign + digits
	}
	return sign + digits + " " + a.Currency
}

// Float64 returns the amount as a float64, for display or approximate
// arithmetic. Use Units and Scale when exactness matters.
func (a CurrencyAmount) Float64() float64 {
	f := float64(a.Units)
	for i := int32(0); i < a.Scale; i++ {
		f /= 10
	}
	return f
}

// parseCurrencyAmount reads a decimal such as "2.75" or "-1.5", ignoring
// surrounding whitespace. ok is false unless s is an optional minus sign,
// digits, and optionally a point and more digits, that fit in an int64.
func parseCurrencyAmount(s, currency string) (a CurrencyAmount, ok bool) {
	s = strings.TrimSpace(s)
	negative := strings.HasPrefix(s, "-")
	whole, fraction, hasPoint := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	if !allDigits(whole) || (hasPoint && !allDigits(fraction)) {
		return CurrencyAmount{}, false
	}
	units, err := strconv.ParseInt(whole+fraction, 10, 64)
	if err != nil {
		return CurrencyAmount{}, false
	}
	if negative {
		units = -units
	}
	return CurrencyAmount{Units: units, Scale: int32(len(fraction)), Currency: currency}, true
}

// allDigits reports whether s is one or more ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
