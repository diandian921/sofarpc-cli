package schema

import (
	"sort"
	"strings"
	"unicode"
)

func Search(idx *Index, query string, limit int, includeOutOfPrefix bool) []Method {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	queryTokens := Tokenize(query)
	var scored []Method
	for _, method := range idx.Methods {
		if method.OutOfPrefix && !includeOutOfPrefix {
			continue
		}
		score, evidence := scoreMethod(method, queryTokens)
		if score == 0 && strings.TrimSpace(query) != "" {
			continue
		}
		m := method
		m.Score = score
		m.Evidence = evidence
		scored = append(scored, m)
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			if scored[i].Service == scored[j].Service {
				return scored[i].Method < scored[j].Method
			}
			return scored[i].Service < scored[j].Service
		}
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
}

func scoreMethod(method Method, queryTokens []string) (int, []string) {
	if len(queryTokens) == 0 {
		return 1, nil
	}
	haystack := strings.Join(append(Tokenize(method.Service+" "+method.Interface+" "+method.Method+" "+method.Summary), strings.ToLower(method.Service), strings.ToLower(method.Method)), " ")
	score := 0
	var evidence []string
	for _, token := range queryTokens {
		if strings.Contains(haystack, token) {
			score += 10
			evidence = append(evidence, token)
		}
	}
	if strings.Contains(strings.ToLower(method.Method), strings.ToLower(strings.Join(queryTokens, ""))) {
		score += 20
	}
	return score, evidence
}

func Tokenize(s string) []string {
	seen := map[string]bool{}
	add := func(token string, out *[]string) {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" || seen[token] {
			return
		}
		seen[token] = true
		*out = append(*out, token)
	}
	var out []string
	for _, part := range splitIdentifier(s) {
		add(part, &out)
		if containsCJK(part) {
			runes := []rune(part)
			if len(runes) > 1 {
				for i := 0; i < len(runes)-1; i++ {
					add(string(runes[i:i+2]), &out)
				}
			}
		}
	}
	return out
}

func splitIdentifier(s string) []string {
	var out []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			out = append(out, string(current))
			current = nil
		}
	}
	var prev rune
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || isCJK(r) {
			if len(current) > 0 && unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
				flush()
			}
			current = append(current, r)
			prev = r
			continue
		}
		flush()
		prev = 0
	}
	flush()
	return out
}

func containsCJK(s string) bool {
	for _, r := range s {
		if isCJK(r) {
			return true
		}
	}
	return false
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}
