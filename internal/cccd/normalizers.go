package cccd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	reWhitespace    = regexp.MustCompile(`\s+`)
	reCommaSpaces   = regexp.MustCompile(`\s*,\s*`)
	reMultiCommas   = regexp.MustCompile(`(,\s*){2,}`)
	reDigitsOrUnder = regexp.MustCompile(`[\d_]+`)
	reIDLabel       = regexp.MustCompile(`(?i)^(số|so|no|id)(?:\s*[:.]\s*|\s+)`)
	reIDSeparators  = regexp.MustCompile(`[\s.\-_/]+`)
	reNonDigit      = regexp.MustCompile(`\D+`)
	reDMY           = regexp.MustCompile(`(\d{1,2})\D+(\d{1,2})\D+(\d{4})`)
	reYMD           = regexp.MustCompile(`(\d{4})\D+(\d{1,2})\D+(\d{1,2})`)
	reCompactDate   = regexp.MustCompile(`^\d{8}$`)
	reDigitRun      = regexp.MustCompile(`[0-9OoIl|SBZ]{1,4}`)
	reNonAlphaNum   = regexp.MustCompile(`[^a-z0-9]+`)
)

var placeholders = map[string]struct{}{
	"n/a":            {},
	"na":             {},
	"null":           {},
	"none":           {},
	"nil":            {},
	"-":              {},
	"--":             {},
	"?":              {},
	"unknown":        {},
	"khong ro":       {},
	"khong doc duoc": {},
}

var confusableDigits = map[rune]rune{
	'O': '0', 'o': '0', 'Q': '0', 'D': '0',
	'I': '1', 'l': '1', '|': '1', 'i': '1',
	'S': '5', 's': '5',
	'B': '8',
	'Z': '2', 'z': '2',
}

var indefiniteMarkers = []string{"khong thoi han", "vo thoi han"}

// StripAccents removes Vietnamese diacritics and converts đ/Đ to d/D.
func StripAccents(text string) string {
	decomposed := norm.NFD.String(text)
	decomposed = strings.ReplaceAll(decomposed, "đ", "d")
	decomposed = strings.ReplaceAll(decomposed, "Đ", "D")
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CleanText normalizes Unicode to NFC, collapses whitespace, trims leading/trailing punctuation,
// and filters out placeholder strings ("N/A", "null", etc.) while preserving valid words like "Không".
func CleanText(value *string) *string {
	if value == nil {
		return nil
	}
	text := norm.NFC.String(*value)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ReplaceAll(text, "\u200b", "")
	text = reWhitespace.ReplaceAllString(text, " ")
	text = strings.Trim(text, " ,;:|_")
	if text == "" {
		return nil
	}
	if _, isPlaceholder := placeholders[strings.ToLower(StripAccents(text))]; isPlaceholder {
		return nil
	}
	return &text
}

// CleanString is a convenience wrapper around CleanText for plain string inputs.
func CleanString(value string) *string {
	return CleanText(&value)
}

// NormalizeTitle converts text to Title Case per word.
func NormalizeTitle(value *string) *string {
	text := CleanText(value)
	if text == nil {
		return nil
	}
	words := strings.Split(*text, " ")
	for i, w := range words {
		runes := []rune(w)
		if len(runes) > 0 {
			words[i] = strings.ToUpper(string(runes[0])) + strings.ToLower(string(runes[1:]))
		}
	}
	res := strings.Join(words, " ")
	return &res
}

// NormalizePlaceText normalizes commas and spacing in addresses/hometowns while preserving original casing.
func NormalizePlaceText(value *string) *string {
	text := CleanText(value)
	if text == nil {
		return nil
	}
	s := reCommaSpaces.ReplaceAllString(*text, ", ")
	s = reMultiCommas.ReplaceAllString(s, ", ")
	s = strings.Trim(s, " ,.;")
	if s == "" {
		return nil
	}
	return &s
}

// NormalizeName strips numbers and extraneous symbols, collapses whitespace, and uppercases the full name.
func NormalizeName(value *string) *string {
	text := CleanText(value)
	if text == nil {
		return nil
	}
	s := reDigitsOrUnder.ReplaceAllString(*text, " ")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsSpace(r) || r == '\'' || r == '’' || r == '.' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	cleaned := reWhitespace.ReplaceAllString(b.String(), " ")
	cleaned = strings.Trim(cleaned, " .'’-")
	cleaned = strings.ToUpper(cleaned)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

// NormalizeID cleans an ID card number and returns (digitsOnly, modificationNotes).
func NormalizeID(value *string) (*string, []string) {
	text := CleanText(value)
	notes := []string{}
	if text == nil {
		return nil, notes
	}
	s := reIDLabel.ReplaceAllString(*text, "")
	compact := reIDSeparators.ReplaceAllString(s, "")

	var fixedBuilder strings.Builder
	fixedBuilder.Grow(len(compact))
	for _, r := range compact {
		if rep, ok := confusableDigits[r]; ok {
			fixedBuilder.WriteRune(rep)
		} else {
			fixedBuilder.WriteRune(r)
		}
	}
	fixed := fixedBuilder.String()
	if fixed != compact {
		notes = append(notes, "Đã sửa ký tự dễ nhầm với chữ số (O→0, I→1...)")
	}

	digits := reNonDigit.ReplaceAllString(fixed, "")
	if digits != fixed {
		notes = append(notes, "Đã loại ký tự không phải chữ số")
	}
	if digits == "" {
		return nil, notes
	}
	return &digits, notes
}

func fixDigitRun(token string) string {
	hasDigit := false
	for _, r := range token {
		if r >= '0' && r <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return token
	}
	var b strings.Builder
	b.Grow(len(token))
	for _, r := range token {
		if rep, ok := confusableDigits[r]; ok {
			b.WriteRune(rep)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParseDate parses a Vietnamese ID card date string into a UTC time.Time (at 00:00:00).
// Returns nil if the date is unparsable or outside 1900..2100.
func ParseDate(value *string) *time.Time {
	text := CleanText(value)
	if text == nil {
		return nil
	}
	s := reDigitRun.ReplaceAllStringFunc(*text, fixDigitRun)

	var day, month, year int
	var matched bool

	if m := reDMY.FindStringSubmatch(s); len(m) == 4 {
		day, _ = strconv.Atoi(m[1])
		month, _ = strconv.Atoi(m[2])
		year, _ = strconv.Atoi(m[3])
		matched = true
	} else if m := reYMD.FindStringSubmatch(s); len(m) == 4 {
		year, _ = strconv.Atoi(m[1])
		month, _ = strconv.Atoi(m[2])
		day, _ = strconv.Atoi(m[3])
		matched = true
	} else if reCompactDate.MatchString(s) {
		day, _ = strconv.Atoi(s[0:2])
		month, _ = strconv.Atoi(s[2:4])
		year, _ = strconv.Atoi(s[4:8])
		matched = true
	}

	if !matched || year < 1900 || year > 2100 || month < 1 || month > 12 || day < 1 || day > 31 {
		return nil
	}

	d := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	// Reject overflow dates such as 31/02/2003
	if d.Year() != year || int(d.Month()) != month || d.Day() != day {
		return nil
	}
	return &d
}

// FormatDate formats a date as dd/mm/yyyy.
func FormatDate(d time.Time) string {
	return fmt.Sprintf("%02d/%02d/%04d", d.Day(), int(d.Month()), d.Year())
}

// NormalizeDate returns (formatted "dd/mm/yyyy", parsed *time.Time, unparsable bool).
func NormalizeDate(value *string) (*string, *time.Time, bool) {
	text := CleanText(value)
	if text == nil {
		return nil, nil, false
	}
	parsed := ParseDate(text)
	if parsed == nil {
		return text, nil, true
	}
	formatted := FormatDate(*parsed)
	return &formatted, parsed, false
}

// NormalizeDOE normalizes the "Date of Expiry" field, recognizing "Không thời hạn".
// Returns (formatted, parsedDate, isIndefinite, unparsable).
func NormalizeDOE(value *string) (*string, *time.Time, bool, bool) {
	text := CleanText(value)
	if text == nil {
		return nil, nil, false, false
	}
	lowered := strings.ToLower(StripAccents(*text))
	for _, marker := range indefiniteMarkers {
		if strings.Contains(lowered, marker) {
			indef := "Không thời hạn"
			return &indef, nil, true, false
		}
	}
	formatted, parsed, bad := NormalizeDate(text)
	return formatted, parsed, false, bad
}

// NormalizeSex returns ("Nam" or "Nữ", unrecognized bool).
func NormalizeSex(value *string) (*string, bool) {
	text := CleanText(value)
	if text == nil {
		return nil, false
	}
	key := strings.ToLower(StripAccents(*text))
	switch key {
	case "nam", "male", "m":
		nam := "Nam"
		return &nam, false
	case "nu", "female", "f":
		nu := "Nữ"
		return &nu, false
	default:
		return nil, true
	}
}

// NormalizeNationality normalizes "Việt Nam" or title-cases other nationalities.
func NormalizeNationality(value *string) *string {
	text := CleanText(value)
	if text == nil {
		return nil
	}
	compact := strings.ReplaceAll(strings.ToLower(StripAccents(*text)), " ", "")
	if compact == "vietnam" || compact == "vn" || compact == "vietnamese" {
		vn := "Việt Nam"
		return &vn
	}
	return NormalizeTitle(text)
}

// Squash strips accents, lowercases, and keeps only alphanumeric tokens separated by single spaces.
func Squash(text string) string {
	s := strings.ToLower(StripAccents(text))
	s = reNonAlphaNum.ReplaceAllString(s, " ")
	s = reWhitespace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
