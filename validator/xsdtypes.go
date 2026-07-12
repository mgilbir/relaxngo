package validator

import (
	"regexp"
	"strconv"
	"strings"
)

// Lexical-space patterns for XSD datatypes. These are compiled once and never
// mutated, so they are safe for concurrent use. They validate the lexical form
// only; where XSD also constrains value ranges (e.g. bounded integers) that is
// handled separately below.
var (
	reXSDInteger  = regexp.MustCompile(`^[+-]?[0-9]+$`)
	reXSDDecimal  = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)$`)
	reXSDDate     = regexp.MustCompile(`^-?[0-9]{4,}-[0-9]{2}-[0-9]{2}(?:Z|[+-][0-9]{2}:[0-9]{2})?$`)
	reXSDDateTime = regexp.MustCompile(`^-?[0-9]{4,}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})?$`)
	reXSDTime     = regexp.MustCompile(`^[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})?$`)
	reXSDGYear    = regexp.MustCompile(`^-?[0-9]{4,}(?:Z|[+-][0-9]{2}:[0-9]{2})?$`)
	reXSDGYearMon = regexp.MustCompile(`^-?[0-9]{4,}-[0-9]{2}(?:Z|[+-][0-9]{2}:[0-9]{2})?$`)
	reXSDGMonth   = regexp.MustCompile(`^--[0-9]{2}(?:Z|[+-][0-9]{2}:[0-9]{2})?$`)
	reXSDGMonDay  = regexp.MustCompile(`^--[0-9]{2}-[0-9]{2}(?:Z|[+-][0-9]{2}:[0-9]{2})?$`)
	reXSDGDay     = regexp.MustCompile(`^---[0-9]{2}(?:Z|[+-][0-9]{2}:[0-9]{2})?$`)
	reXSDDuration = regexp.MustCompile(`^-?P(?:[0-9]+Y)?(?:[0-9]+M)?(?:[0-9]+D)?(?:T(?:[0-9]+H)?(?:[0-9]+M)?(?:[0-9]+(?:\.[0-9]+)?S)?)?$`)
	reXSDHex      = regexp.MustCompile(`^(?:[0-9a-fA-F]{2})*$`)
	reXSDBase64   = regexp.MustCompile(`^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$`)
	reXSDLanguage = regexp.MustCompile(`^[a-zA-Z]{1,8}(?:-[a-zA-Z0-9]{1,8})*$`)
	reXSDNCName   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	reXSDName     = regexp.MustCompile(`^[A-Za-z_:][A-Za-z0-9_.:-]*$`)
	reXSDNMToken  = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)
)

// validateXSDType reports whether value is in the lexical (and, where
// applicable, value) space of the named XSD datatype. Types that require
// context that is not available here (QName, NOTATION) or that are effectively
// unconstrained (anyURI) return true. Unknown/unsupported types also return
// true so that validation stays conservative rather than rejecting content it
// does not understand.
func validateXSDType(typeName, value string) bool {
	switch typeName {
	case "boolean":
		return value == "true" || value == "false" || value == "1" || value == "0"

	// Bounded signed integers: lexical integer plus range.
	case "long":
		return inSignedRange(value, 64)
	case "int":
		return inSignedRange(value, 32)
	case "short":
		return inSignedRange(value, 16)
	case "byte":
		return inSignedRange(value, 8)

	// Bounded unsigned integers.
	case "unsignedLong":
		return inUnsignedRange(value, 64)
	case "unsignedInt":
		return inUnsignedRange(value, 32)
	case "unsignedShort":
		return inUnsignedRange(value, 16)
	case "unsignedByte":
		return inUnsignedRange(value, 8)

	// Arbitrary-precision integers with sign/zero constraints.
	case "integer":
		return reXSDInteger.MatchString(value)
	case "nonNegativeInteger":
		return reXSDInteger.MatchString(value) && !isNegativeInt(value)
	case "positiveInteger":
		return reXSDInteger.MatchString(value) && !isNegativeInt(value) && !isZeroInt(value)
	case "negativeInteger":
		return reXSDInteger.MatchString(value) && isNegativeInt(value) && !isZeroInt(value)
	case "nonPositiveInteger":
		return reXSDInteger.MatchString(value) && (isNegativeInt(value) || isZeroInt(value))

	case "decimal":
		// No exponent, no INF/NaN — distinct from double/float.
		return reXSDDecimal.MatchString(value)
	case "double", "float":
		_, err := strconv.ParseFloat(value, 64)
		return err == nil

	case "date":
		return reXSDDate.MatchString(value)
	case "dateTime":
		return reXSDDateTime.MatchString(value)
	case "time":
		return reXSDTime.MatchString(value)
	case "gYear":
		return reXSDGYear.MatchString(value)
	case "gYearMonth":
		return reXSDGYearMon.MatchString(value)
	case "gMonth":
		return reXSDGMonth.MatchString(value)
	case "gMonthDay":
		return reXSDGMonDay.MatchString(value)
	case "gDay":
		return reXSDGDay.MatchString(value)
	case "duration":
		// A duration must have at least one component (reject bare "P"/"PT").
		return reXSDDuration.MatchString(value) && strings.ContainsAny(value, "0123456789")

	case "hexBinary":
		return reXSDHex.MatchString(value)
	case "base64Binary":
		return reXSDBase64.MatchString(strings.ReplaceAll(value, " ", ""))
	case "language":
		return reXSDLanguage.MatchString(value)
	case "Name":
		return reXSDName.MatchString(value)
	case "NCName", "ID", "IDREF", "ENTITY":
		return reXSDNCName.MatchString(value)
	case "NMTOKEN":
		return reXSDNMToken.MatchString(value)

	case "anyURI", "QName", "NOTATION":
		return true

	default:
		return true
	}
}

func inSignedRange(v string, bits int) bool {
	_, err := strconv.ParseInt(v, 10, bits)
	return err == nil
}

func inUnsignedRange(v string, bits int) bool {
	// XSD unsigned lexical space permits an optional leading '+'.
	_, err := strconv.ParseUint(strings.TrimPrefix(v, "+"), 10, bits)
	return err == nil
}

// isNegativeInt reports whether a lexically-valid integer is negative (a leading
// '-' on a non-zero magnitude). "-0" is not negative.
func isNegativeInt(v string) bool {
	return strings.HasPrefix(v, "-") && !isZeroInt(v)
}

// isZeroInt reports whether a lexically-valid integer has magnitude zero.
func isZeroInt(v string) bool {
	mag := strings.TrimLeft(v, "+-")
	return strings.Trim(mag, "0") == ""
}
