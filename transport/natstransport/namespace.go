package natstransport

import (
	"strings"
	"unicode"
)

const NatsSubjectNamespace = "eurus"

func namespace(strValues ...string) string {
	namespaceChunks := []string{NatsSubjectNamespace}
	for _, str := range strValues {
		if str != "" {
			namespaceChunks = append(namespaceChunks, formatForNamespace(str))
		}
	}
	return strings.Join(namespaceChunks, ".")
}

func formatForNamespace(str string) string {
	// Build into a byte builder sized to the input. All characters emitted are
	// ASCII (camelCase boundaries insert at most one extra '-'), so a builder
	// grown to len(str) covers the common case without reallocation.
	var b strings.Builder
	b.Grow(len(str) + 1)

	var pr rune
	for _, r := range str {
		switch {
		case r >= 'A' && r <= 'Z':
			if pr >= 'a' && pr <= 'z' {
				b.WriteByte('-')
				b.WriteRune(unicode.ToLower(r))
			} else {
				b.WriteRune(r)
			}
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-':
			b.WriteByte('-')
		case r == '_':
			b.WriteByte('-')
		case r == '.':
			b.WriteByte('.')
		case r == '*':
			b.WriteByte('*')
		}
		pr = r
	}
	return b.String()
}
