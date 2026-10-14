package official

import (
	"reflect"
	"testing"
)

func TestLocalizedChangelogSubmitsOnlyOriginalLanguage(t *testing.T) {
	for _, test := range []struct {
		name, text, language string
		locales              []string
	}{
		{name: "English without locale configuration", text: "Fix login and improve search", language: "en"},
		{name: "Chinese without locale configuration", text: "修复登录问题，优化搜索体验", language: "zh"},
		{name: "legacy bilingual configuration", text: "Release notes", locales: []string{"zh", "en"}, language: "en"},
		{name: "legacy wrong Chinese label", text: "Fix login", locales: []string{"zh"}, language: "en"},
		{name: "legacy wrong English label", text: "修复登录问题", locales: []string{"en"}, language: "zh"},
		{name: "generic fallback", text: "Release 1.2.3", locales: []string{"zh", "en"}, language: "en"},
		{name: "uppercase English", text: "FIX LOGIN ERRORS", language: "en"},
		{name: "short uppercase English", text: "FIX BUG", language: "en"},
		{name: "hyphenated English prose", text: "Bug-fixes", language: "en"},
		{name: "generated header with Chinese release body", text: "Upstream 1.2.3\n修复", language: "zh"},
		{name: "generated header with Chinese commits", text: "Upstream v1.2.3 (since 1.2.2)\n- fix(ui): 修复\n- feat: 新增搜索", language: "zh"},
		{name: "Chinese technical notes", text: "修复 Docker 启动失败，支持 GitHub API 和 Jenkins 配置", language: "zh"},
		{name: "English with a Chinese example", text: "Fix the display of the Chinese text 中文 in the editor", language: "en"},
		{name: "Chinese dominant mixed release", text: "## Changes\n- 修复登录失败，优化应用初始化体验\n- Fix typo", language: "zh"},
		{name: "English dominant mixed release", text: "## Changes\n- Fix login errors and improve search performance\n- 修复", language: "en"},
		{name: "inline code and identifiers", text: "修复 `HTTP request failed` 和 cloud.lazycat.app.example 启动问题", language: "zh"},
		{name: "fenced English code", text: "修复\n```go\nfunc main() { fmt.Println(\"Release notes and example code\") }\n```", language: "zh"},
		{name: "fenced Chinese code", text: "Fix login errors\n~~~text\n中文代码示例不会改变日志的主体语言\n~~~", language: "en"},
		{name: "longer fence does not close early", text: "Fix login\n````text\n```\n中文代码仍然处于代码块中\n````", language: "en"},
		{name: "Chinese link label", text: "修复 [登录](https://github.com/example/project/pull/123)", language: "zh"},
		{name: "Chinese URL is not prose", text: "Fix login https://example.com/中文地址", language: "en"},
		{name: "English URLs are not prose", text: "修复 https://github.com/example/application/releases/tag/v1.2.3", language: "zh"},
		{name: "Chinese prose immediately after a URL", text: "https://example.com，修复登录", language: "zh"},
		{name: "image and HTML code", text: "Fix login\n![中文截图说明](image.png)\n<code>很长的中文代码示例</code><!-- 中文注释 -->", language: "en"},
		{name: "HTML prose", text: "<p>修复登录</p><pre>long English example code</pre>", language: "zh"},
		{name: "CRLF", text: "Upstream 1.2.3\r\n- 修复", language: "zh"},
		{name: "ordinary upstream text is not a generated header", text: "Upstream fixes", language: "en"},
		{name: "unclassified numeric note with explicit hint", text: "1.2.3 → 1.2.4", locales: []string{" ZH ", "zh"}, language: "zh"},
		{name: "unclassified symbol note with explicit hint", text: "✅", locales: []string{"EN"}, language: "en"},
		{name: "original formatting retained", text: "\n  修复登录问题\n\n", language: "zh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := localizedChangelog(test.text, test.locales)
			if err != nil || !reflect.DeepEqual(got, map[string]string{test.language: test.text}) {
				t.Fatalf("changelogs=%#v error=%v", got, err)
			}
		})
	}
}

func TestLocalizedChangelogDoesNotInventLanguage(t *testing.T) {
	for _, text := range []string{"", "123", "✅", "https://example.com/中文地址", "Upstream 1.2.3", "```text\n修复登录\n```", "ログインを修正", "로그인 수정"} {
		for _, locales := range [][]string{nil, {"zh", "en"}, {"", " "}, {"fr"}} {
			if got, err := localizedChangelog(text, locales); err == nil || got != nil {
				t.Fatalf("text=%q locales=%v changelogs=%#v error=%v", text, locales, got, err)
			}
		}
	}
}
