// Package compiler converts Markdown TSGs into schema-valid runbooks.
package compiler

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Section represents a parsed section from a TSG document.
type Section struct {
	Heading    string      // Section heading text
	Level      int         // Heading level (1-6)
	CodeBlocks []CodeBlock // Code blocks found in this section
	Paragraphs []string    // Prose paragraphs
	ListItems  []string    // Checklist / list items
	StartLine  int         // Source line number (0-based)
}

// CodeBlock represents a fenced code block extracted from Markdown.
type CodeBlock struct {
	Language string // Language identifier (e.g., "bash", "sh")
	Content  string // Raw code content
	Line     int    // Source line number
}

// IR is the intermediate representation of a parsed TSG.
type IR struct {
	Title    string    // Document title (first H1)
	Sections []Section // Extracted sections
	Vars     []string  // Extracted variable names ($VAR patterns)
}

// varPattern matches $VAR_NAME or ${VAR_NAME} patterns in text.
var varPattern = regexp.MustCompile(`\$\{?([A-Z][A-Z0-9_]*)\}?`)

// unsafeCommands are commands that should produce manual steps.
var unsafeCommands = map[string]bool{
	"rm":    true,
	"rmdir": true,
	"dd":    true,
	"mkfs":  true,
	"fdisk": true,
	"sudo":  true,
	"chmod": true,
	"chown": true,
}

// ParseTSG parses a Markdown TSG document into an intermediate representation.
func ParseTSG(source []byte) (*IR, error) {
	parser := goldmark.DefaultParser()
	reader := text.NewReader(source)
	doc := parser.Parse(reader)

	ir := &IR{}
	varSet := make(map[string]bool)

	var currentSection *Section

	err := ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.Heading:
			heading := extractText(n, source)
			section := Section{
				Heading:   heading,
				Level:     n.Level,
				StartLine: lineNumber(source, n),
			}

			if ir.Title == "" && n.Level == 1 {
				ir.Title = heading
			}

			ir.Sections = append(ir.Sections, section)
			currentSection = &ir.Sections[len(ir.Sections)-1]
			return ast.WalkSkipChildren, nil

		case *ast.FencedCodeBlock:
			if currentSection == nil {
				return ast.WalkContinue, nil
			}
			lang := string(n.Language(source))
			content := extractCodeContent(n, source)

			currentSection.CodeBlocks = append(currentSection.CodeBlocks, CodeBlock{
				Language: lang,
				Content:  content,
				Line:     lineNumber(source, n),
			})

			// Extract variables from code block
			for _, matches := range varPattern.FindAllStringSubmatch(content, -1) {
				varSet[matches[1]] = true
			}

			return ast.WalkSkipChildren, nil

		case *ast.Paragraph:
			if currentSection == nil {
				return ast.WalkContinue, nil
			}
			text := extractText(n, source)
			if text != "" {
				currentSection.Paragraphs = append(currentSection.Paragraphs, text)
				// Extract variables from prose
				for _, matches := range varPattern.FindAllStringSubmatch(text, -1) {
					varSet[matches[1]] = true
				}
			}
			return ast.WalkSkipChildren, nil

		case *ast.ListItem:
			if currentSection == nil {
				return ast.WalkContinue, nil
			}
			itemText := extractText(n, source)
			if itemText != "" {
				currentSection.ListItems = append(currentSection.ListItems, itemText)
			}
			return ast.WalkSkipChildren, nil
		}

		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil, err
	}

	// Collect unique variable names
	for v := range varSet {
		ir.Vars = append(ir.Vars, v)
	}

	return ir, nil
}

// IsUnsafeCommand checks if a command is considered unsafe/destructive.
func IsUnsafeCommand(cmd string) bool {
	// Extract base command (first word)
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}
	base := parts[0]
	// Handle paths like /usr/bin/rm
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	return unsafeCommands[base]
}

// extractText extracts the full text content from a node.
func extractText(node ast.Node, source []byte) string {
	var sb strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *ast.Text:
			sb.Write(c.Segment.Value(source))
			if c.SoftLineBreak() {
				sb.WriteByte(' ')
			}
		case *ast.CodeSpan:
			// Extract inline code
			for gc := c.FirstChild(); gc != nil; gc = gc.NextSibling() {
				if t, ok := gc.(*ast.Text); ok {
					sb.Write(t.Segment.Value(source))
				}
			}
		default:
			// Recursively extract text from other inline elements
			sb.WriteString(extractText(child, source))
		}
	}
	return strings.TrimSpace(sb.String())
}

// extractCodeContent extracts the raw content from a fenced code block.
func extractCodeContent(n *ast.FencedCodeBlock, source []byte) string {
	var sb strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		sb.Write(line.Value(source))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// lineNumber returns the 0-based line number for a node.
func lineNumber(source []byte, node ast.Node) int {
	// Find the byte offset of the node
	if node.Lines().Len() > 0 {
		line := node.Lines().At(0)
		return countNewlines(source[:line.Start])
	}
	// For headings and other nodes without Lines()
	if node.HasChildren() {
		child := node.FirstChild()
		if t, ok := child.(*ast.Text); ok {
			return countNewlines(source[:t.Segment.Start])
		}
	}
	return 0
}

// countNewlines counts the number of newline characters in a byte slice.
func countNewlines(b []byte) int {
	count := 0
	for _, c := range b {
		if c == '\n' {
			count++
		}
	}
	return count
}
