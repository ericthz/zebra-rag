// Package chunker 提供文本分块策略。
// 支持固定窗口 + 重叠（baseline）与 Markdown 结构分块（按标题层级切分，块过大时回退固定窗口）。
package chunker

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Chunk 表示一个文本分块。
type Chunk struct {
	Index      int    // 分块序号
	Heading    string // 所属标题（结构分块时用于补充上下文，可为空）
	Text       string // 分块内容（小块，用于检索）
	ParentText string // 父块内容（整节/整段，用于生成上下文；空表示无父块）
}

// Chunker 定义文本分块器接口。
type Chunker interface {
	// Chunk 将长文本切分为若干分块。
	Chunk(text string, chunkSize, chunkOverlap int) ([]Chunk, error)
}

// New 根据策略名创建分块器：fixed（固定窗口）或 markdown（结构分块）。
// 默认返回 markdown 分块器。
func New(strategy string) Chunker {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "fixed", "window":
		return &FixedWindowChunker{}
	default:
		return &MarkdownChunker{}
	}
}

// FixedWindowChunker 固定窗口 + 重叠切分（按 rune 处理，避免切坏多字节字符）。
type FixedWindowChunker struct{}

// Chunk 实现 Chunker 接口。
func (c *FixedWindowChunker) Chunk(text string, chunkSize, chunkOverlap int) ([]Chunk, error) {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil, nil
	}
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if chunkOverlap < 0 || chunkOverlap >= chunkSize {
		chunkOverlap = 0
	}
	step := chunkSize - chunkOverlap
	if step <= 0 {
		step = chunkSize
	}
	var chunks []Chunk
	idx := 0
	for i := 0; i < len(runes); i += step {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, Chunk{Index: idx, Text: string(runes[i:end])})
		idx++
		if end == len(runes) {
			break
		}
	}
	return chunks, nil
}

var headerRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)

// section 表示按标题切分出的一个段落。
type section struct {
	Level int    // 标题级别（1-6），0 表示无标题的引言段
	Title string // 标题文本
	Body  string // 段内正文
}

// MarkdownChunker 结构分块：按 Markdown 标题层级切分段落，
// 段落过长时回退到固定窗口，并为每个分块带上标题作为上下文。
type MarkdownChunker struct{}

// Chunk 实现 Chunker 接口。
func (c *MarkdownChunker) Chunk(text string, chunkSize, chunkOverlap int) ([]Chunk, error) {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	sections := splitSections(text)
	if len(sections) == 0 {
		return nil, nil
	}
	fixed := &FixedWindowChunker{}
	var chunks []Chunk
	for _, sec := range sections {
		full := sec.Body
		heading := sec.Title
		if heading != "" {
			// 标题作为上下文前缀，帮助模型理解块内主题
			headingPrefix := strings.Repeat("#", sec.Level) + " " + heading + "\n"
			full = headingPrefix + sec.Body
		}
		parent := strings.TrimSpace(full)
		if utf8.RuneCountInString(full) <= chunkSize {
			chunks = append(chunks, Chunk{Index: len(chunks), Heading: heading, Text: parent, ParentText: parent})
			continue
		}
		// 段过长：按固定窗口拆，每块都带标题上下文
		parts, err := fixed.Chunk(full, chunkSize, chunkOverlap)
		if err != nil {
			return nil, err
		}
		for _, p := range parts {
			chunks = append(chunks, Chunk{Index: len(chunks), Heading: heading, Text: strings.TrimSpace(p.Text), ParentText: parent})
		}
	}
	return chunks, nil
}

// splitSections 按 Markdown 标题切分文本为若干段落。
// 首个标题之前的内容作为引言段（Level=0）。
func splitSections(text string) []section {
	lines := strings.Split(text, "\n")
	var sections []section
	var cur *section

	flush := func() {
		if cur != nil && strings.TrimSpace(cur.Body) != "" {
			sections = append(sections, *cur)
		}
	}

	for _, line := range lines {
		if m := headerRe.FindStringSubmatch(line); m != nil {
			flush()
			cur = &section{Level: len(m[1]), Title: strings.TrimSpace(m[2]), Body: ""}
			continue
		}
		if cur == nil {
			cur = &section{Level: 0, Title: "", Body: ""}
		}
		cur.Body += line + "\n"
	}
	flush()
	return sections
}
