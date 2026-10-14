package official

import (
	"regexp"
	"strings"
	"unicode"
)

type changelogLanguageError struct{}

func (changelogLanguageError) Error() string {
	return "unable to determine changelog language; provide Chinese or English prose, or set a single changelog_locales hint to [zh] or [en]"
}

func (err changelogLanguageError) PublicErrorDetail() string { return err.Error() }

var (
	changelogHTMLCode = regexp.MustCompile(`(?is)<pre\b[^>]*>.*?</pre\s*>|<code\b[^>]*>.*?</code\s*>|<!--.*?-->`)
	changelogHTMLTag  = regexp.MustCompile(`<[^>\n]+>`)
	changelogImage    = regexp.MustCompile(`!\[[^\]\n]*\]\([^\n)]*\)`)
	changelogLink     = regexp.MustCompile(`\[([^\]\n]*)\]\([^\n)]*\)`)
	changelogURL      = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s<>()，。；、！？」”]+`)
	changelogCode     = regexp.MustCompile("`+[^`\n]*`+")
	changelogHeader   = regexp.MustCompile(`^Upstream v?[0-9][A-Za-z0-9.+-]*(?: \(since [A-Za-z0-9.+-]+\))?$`)
	changelogCommit   = regexp.MustCompile(`(?i)^(?:feat|fix|chore|docs|refactor|perf|test|build|ci|style|revert)(?:\([^)]*\))?!?:\s*`)
	changelogWord     = regexp.MustCompile(`[A-Za-z][A-Za-z0-9._/@:+-]*`)
)

// localizedChangelog labels the original once; the store fills missing languages.
// Legacy locale lists are hints for ambiguous text, never translation targets.
func localizedChangelog(text string, locales []string) (map[string]string, error) {
	language := detectChangelogLanguage(text)
	if language == "" {
		language = changelogLanguageHint(locales)
	}
	if language == "" {
		return nil, changelogLanguageError{}
	}
	return map[string]string{language: text}, nil
}

// This is a lightweight zh/en heuristic, not a general language classifier.
// A 2:1 Han-to-prose-word threshold allows English names in Chinese notes.
// Technical identifiers and markup are excluded from that comparison.
func detectChangelogLanguage(text string) string {
	text = changelogHTMLCode.ReplaceAllString(text, " ")
	var prose strings.Builder
	var fence byte
	var fenceLength int
	for index, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if index == 0 && changelogHeader.MatchString(line) {
			continue
		}
		if fence != 0 {
			if strings.HasPrefix(line, strings.Repeat(string(fence), fenceLength)) && strings.Trim(line, string(fence)+" \t") == "" {
				fence = 0
			}
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			fence = line[0]
			fenceLength = len(line) - len(strings.TrimLeft(line, string(fence)))
			continue
		}
		line = changelogCommit.ReplaceAllString(strings.TrimLeft(line, "-*+># \t"), "")
		prose.WriteString(line)
		prose.WriteByte('\n')
	}
	text = changelogImage.ReplaceAllString(prose.String(), " ")
	text = changelogLink.ReplaceAllString(text, "$1")
	text = changelogURL.ReplaceAllString(text, " ")
	text = changelogHTMLTag.ReplaceAllString(text, " ")
	text = changelogCode.ReplaceAllString(text, " ")
	chinese, englishWords, latinWords := 0, 0, 0
	for _, char := range text {
		if unicode.Is(unicode.Han, char) {
			chinese++
		} else if unicode.IsLetter(char) && !unicode.Is(unicode.Latin, char) {
			// Do not mistake Japanese/Korean/other scripts for Chinese or English.
			return ""
		}
	}
	for _, word := range changelogWord.FindAllString(text, -1) {
		word = strings.Trim(word, ".:+-")
		upper, identifier := 0, false
		for _, char := range word {
			if unicode.IsUpper(char) {
				upper++
			}
			if !unicode.IsLetter(char) && char != '-' {
				identifier = true
			}
		}
		if !identifier && len(word) > 1 {
			latinWords++
			if upper <= 1 || upper == len(word) && len(word) > 4 {
				englishWords++
			}
		}
	}
	if chinese > 0 && chinese >= 2*englishWords {
		return "zh"
	}
	if latinWords > 0 {
		return "en"
	}
	return ""
}

func changelogLanguageHint(locales []string) string {
	hint := ""
	for _, locale := range locales {
		locale = strings.ToLower(strings.TrimSpace(locale))
		if locale == "" {
			continue
		}
		if locale != "zh" && locale != "en" || hint != "" && hint != locale {
			return ""
		}
		hint = locale
	}
	return hint
}
