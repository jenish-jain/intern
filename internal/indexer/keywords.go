package indexer

import (
	"regexp"
	"strings"
)

// Common English stop words that don't add semantic value
var stopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "but": true, "by": true, "for": true, "if": true, "in": true,
	"into": true, "is": true, "it": true, "no": true, "not": true, "of": true,
	"on": true, "or": true, "such": true, "that": true, "the": true, "their": true,
	"then": true, "there": true, "these": true, "they": true, "this": true, "to": true,
	"was": true, "will": true, "with": true, "when": true, "where": true, "which": true,
}

// ExtractKeywords extracts meaningful keywords from ticket description
// Returns a slice of normalized keywords useful for file matching
func ExtractKeywords(text string) []string {
	if text == "" {
		return nil
	}

	keywords := make(map[string]bool)

	// Extract file paths (e.g., "internal/auth/login.go")
	filePaths := extractFilePaths(text)
	for _, path := range filePaths {
		keywords[path] = true

		// Also extract individual path segments for partial matching
		segments := strings.Split(path, "/")
		for _, seg := range segments {
			seg = strings.TrimSpace(seg)
			if seg != "" && len(seg) > 2 {
				keywords[strings.ToLower(seg)] = true
			}
		}
	}

	// Extract technical terms and camelCase/snake_case identifiers
	identifiers := extractIdentifiers(text)
	for _, id := range identifiers {
		keywords[id] = true
	}

	// Extract regular words (filter stop words)
	words := extractWords(text)
	for _, word := range words {
		word = strings.ToLower(word)
		if len(word) > 2 && !stopWords[word] {
			keywords[word] = true
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(keywords))
	for kw := range keywords {
		result = append(result, kw)
	}

	return result
}

// extractFilePaths finds file path patterns in text
// Matches patterns like: internal/auth/login.go, src/components/Button.tsx
func extractFilePaths(text string) []string {
	// Match paths with directory separators and file extensions
	pathRegex := regexp.MustCompile(`\b([a-zA-Z0-9_\-]+/[a-zA-Z0-9_\-/]*\.[a-zA-Z0-9]+)\b`)
	matches := pathRegex.FindAllStringSubmatch(text, -1)

	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			paths = append(paths, match[1])
		}
	}

	return paths
}

// extractIdentifiers finds camelCase, PascalCase, and snake_case identifiers
// These often represent function names, types, or variables mentioned in tickets
func extractIdentifiers(text string) []string {
	identifiers := make(map[string]bool)

	// Match camelCase (e.g., getUserData) - starts with lowercase
	camelRegex := regexp.MustCompile(`\b([a-z]+[A-Z][a-zA-Z0-9]*)\b`)
	matches := camelRegex.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) > 1 {
			id := match[1]
			identifiers[strings.ToLower(id)] = true

			// Split camelCase into parts for partial matching
			// e.g., "getUserData" -> ["get", "user", "data"]
			parts := splitCamelCase(id)
			for _, part := range parts {
				if len(part) > 2 && !stopWords[part] {
					identifiers[part] = true
				}
			}
		}
	}

	// Match PascalCase (e.g., AuthService) - starts with uppercase
	pascalRegex := regexp.MustCompile(`\b([A-Z][a-z]+[A-Z][a-zA-Z0-9]*)\b`)
	matches = pascalRegex.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) > 1 {
			id := match[1]
			identifiers[strings.ToLower(id)] = true

			// Split PascalCase into parts for partial matching
			// e.g., "AuthService" -> ["auth", "service"]
			parts := splitCamelCase(id)
			for _, part := range parts {
				if len(part) > 2 && !stopWords[part] {
					identifiers[part] = true
				}
			}
		}
	}

	// Match snake_case (e.g., user_service, get_auth_token)
	snakeRegex := regexp.MustCompile(`\b([a-z][a-z0-9_]+[a-z0-9])\b`)
	matches = snakeRegex.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) > 1 && strings.Contains(match[1], "_") {
			id := match[1]
			identifiers[id] = true

			// Also extract individual parts
			parts := strings.Split(id, "_")
			for _, part := range parts {
				if len(part) > 2 && !stopWords[part] {
					identifiers[part] = true
				}
			}
		}
	}

	result := make([]string, 0, len(identifiers))
	for id := range identifiers {
		result = append(result, id)
	}

	return result
}

// extractWords extracts individual words from text
func extractWords(text string) []string {
	// Match word characters
	wordRegex := regexp.MustCompile(`\b([a-zA-Z]{3,})\b`)
	matches := wordRegex.FindAllStringSubmatch(text, -1)

	words := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			words = append(words, match[1])
		}
	}

	return words
}

// splitCamelCase splits camelCase or PascalCase into individual words
// Example: "getUserData" -> ["get", "user", "data"]
func splitCamelCase(s string) []string {
	var result []string
	var current strings.Builder

	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			if current.Len() > 0 {
				result = append(result, strings.ToLower(current.String()))
				current.Reset()
			}
		}
		current.WriteRune(r)
	}

	if current.Len() > 0 {
		result = append(result, strings.ToLower(current.String()))
	}

	return result
}

// NormalizeKeyword normalizes a keyword for consistent matching
func NormalizeKeyword(keyword string) string {
	// Convert to lowercase
	keyword = strings.ToLower(keyword)

	// Remove common file extensions for matching
	keyword = strings.TrimSuffix(keyword, ".go")
	keyword = strings.TrimSuffix(keyword, ".md")
	keyword = strings.TrimSuffix(keyword, ".json")
	keyword = strings.TrimSuffix(keyword, ".yaml")
	keyword = strings.TrimSuffix(keyword, ".yml")

	// Trim special characters
	keyword = strings.Trim(keyword, ".,;:!?()[]{}\"'")

	return keyword
}
