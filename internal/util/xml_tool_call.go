package util

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

const xmlToolCallOpenTag = "<tool_call"

var (
	xmlToolCallBlockPattern  = regexp.MustCompile(`(?is)<tool_call>(.*?)</tool_call>`)
	xmlInvokePattern         = regexp.MustCompile(`(?is)<invoke\s+name=(?:"([^"]+)"|'([^']+)')\s*>(.*?)</invoke>`)
	xmlInvokeIncompletePattern = regexp.MustCompile(`(?is)<invoke\s+name=(?:"([^"]+)"|'([^']+)')\s*>(.*)`)
)

// XMLToolInvoke is a tool call encoded as <invoke name="...">child tags</invoke>.
type XMLToolInvoke struct {
	Name string
	Args map[string]any
}

// HasXMLToolCallMarkup reports whether content contains MiniMax-style XML tool calls.
func HasXMLToolCallMarkup(content string) bool {
	return strings.Contains(strings.ToLower(content), xmlToolCallOpenTag)
}

// ProseBeforeXMLToolCalls returns the assistant prose before the first <tool_call> tag.
func ProseBeforeXMLToolCalls(content string) string {
	lower := strings.ToLower(content)
	idx := strings.Index(lower, xmlToolCallOpenTag)
	if idx < 0 {
		return content
	}
	return content[:idx]
}

// ParseXMLToolCalls extracts <tool_call><invoke .../></tool_call> blocks from content.
func ParseXMLToolCalls(content string) []XMLToolInvoke {
	if !HasXMLToolCallMarkup(content) {
		return nil
	}

	var invokes []XMLToolInvoke
	for _, block := range xmlToolCallBlockPattern.FindAllStringSubmatch(content, -1) {
		if len(block) < 2 {
			continue
		}
		invokes = append(invokes, parseXMLInvokeBlock(block[1])...)
	}

	if len(invokes) == 0 {
		// Stream may end before </tool_call>; try parsing a trailing opener.
		lower := strings.ToLower(content)
		start := strings.LastIndex(lower, xmlToolCallOpenTag)
		if start >= 0 {
			invokes = append(invokes, parseXMLInvokeBlock(content[start:])...)
		}
	}
	return invokes
}

func parseXMLInvokeBlock(block string) []XMLToolInvoke {
	var invokes []XMLToolInvoke
	for _, match := range xmlInvokePattern.FindAllStringSubmatch(block, -1) {
		if len(match) < 4 {
			continue
		}
		if inv, ok := xmlInvokeFromMatch(match[1], match[2], match[3]); ok {
			invokes = append(invokes, inv)
		}
	}
	if len(invokes) > 0 {
		return invokes
	}
	// Upstream may truncate before </invoke> (max_tokens). Still emit a tool_use
	// with whatever arguments were fully closed before the cut.
	if match := xmlInvokeIncompletePattern.FindStringSubmatch(block); len(match) >= 4 {
		if inv, ok := xmlInvokeFromMatch(match[1], match[2], match[3]); ok {
			invokes = append(invokes, inv)
		}
	}
	return invokes
}

func xmlInvokeFromMatch(name1, name2, body string) (XMLToolInvoke, bool) {
	name := strings.TrimSpace(name1)
	if name == "" {
		name = strings.TrimSpace(name2)
	}
	if name == "" {
		return XMLToolInvoke{}, false
	}
	return XMLToolInvoke{Name: name, Args: parseXMLElements(body)}, true
}

func parseXMLElements(body string) map[string]any {
	args := make(map[string]any)
	i := 0
	for i < len(body) {
		if body[i] != '<' {
			i++
			continue
		}
		closeAngle := strings.IndexByte(body[i+1:], '>')
		if closeAngle < 0 {
			break
		}
		tag := strings.TrimSpace(body[i+1 : i+1+closeAngle])
		if tag == "" || strings.HasPrefix(tag, "/") || strings.ContainsAny(tag, " \t\r\n") {
			i++
			continue
		}
		valueStart := i + 1 + closeAngle + 1
		closeTag := "</" + tag + ">"
		closeIdx := strings.Index(body[valueStart:], closeTag)
		if closeIdx < 0 {
			// Truncated stream: capture remainder of the last opened tag.
			remainder := body[valueStart:]
			if strings.TrimSpace(remainder) != "" {
				args[tag] = coerceXMLToolArgValue(strings.TrimSpace(remainder))
			}
			break
		}
		args[tag] = coerceXMLToolArgValue(strings.TrimSpace(body[valueStart : valueStart+closeIdx]))
		i = valueStart + closeIdx + len(closeTag)
	}
	return args
}

func coerceXMLToolArgValue(raw string) any {
	if raw == "true" {
		return true
	}
	if raw == "false" {
		return false
	}
	if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f
	}
	return raw
}

// ArgumentsJSON marshals invoke args for tool_use input_json_delta.
func (inv XMLToolInvoke) ArgumentsJSON() string {
	if len(inv.Args) == 0 {
		return "{}"
	}
	out, err := json.Marshal(inv.Args)
	if err != nil {
		return "{}"
	}
	return string(out)
}