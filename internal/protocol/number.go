package protocol

import (
	"math"
	"strconv"
	"strings"
)

// NumberString renders a binary64 value with JavaScript Number.toString's
// decimal/exponent cutoffs. Commands and Buffer diagnostics share this format.
func NumberString(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "Infinity"
	case math.IsInf(v, -1):
		return "-Infinity"
	case v == 0:
		return "0"
	}
	a := math.Abs(v)
	if a >= 1e-6 && a < 1e21 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	s := strconv.FormatFloat(v, 'e', -1, 64)
	s = strings.Replace(s, "e-0", "e-", 1)
	s = strings.Replace(s, "e+0", "e+", 1)
	return s
}

// Node's ERR_OUT_OF_RANGE groups integer values strictly above 2^32. It even
// groups exponent strings (1e21 becomes "1e_+21"); retain that observable quirk.
func bufferNumberString(v float64) string {
	s := NumberString(v)
	if math.IsInf(v, 0) || math.IsNaN(v) || math.Trunc(v) != v || math.Abs(v) <= 4294967296 {
		return s
	}
	first := 0
	if s[0] == '-' {
		first = 1
	}
	for i := len(s) - 3; i > first; i -= 3 {
		s = s[:i] + "_" + s[i:]
	}
	return s
}
