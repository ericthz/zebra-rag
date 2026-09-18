// Package tokencount 提供轻量的 Token 数估算能力（无外部依赖）。
// 用于知识库列表展示“预估/实际向量化”的 Token 消耗量。
package tokencount

import "unicode"

// Estimate 估算一段文本的 token 数量。
// 启发式口径：中日韩字符约 1 token/字，其余字符约 4 字符/token（与主流 embedding 分词规模接近）。
func Estimate(text string) int {
	if text == "" {
		return 0
	}
	tokens := 0
	other := 0
	for _, r := range text {
		if isCJK(r) {
			tokens++
		} else {
			other++
		}
	}
	tokens += (other + 3) / 4
	if tokens == 0 {
		tokens = 1
	}
	return tokens
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
