package thinking

import (
	"github.com/tidwall/gjson"
)

// GetThinkingText extracts the thinking text from a content part.
func GetThinkingText(part gjson.Result) string {
	if text := part.Get("text"); text.Exists() && text.Type == gjson.String {
		return text.String()
	}

	thinkingField := part.Get("thinking")
	if !thinkingField.Exists() {
		return ""
	}

	if thinkingField.Type == gjson.String {
		return thinkingField.String()
	}

	if thinkingField.IsObject() {
		if inner := thinkingField.Get("text"); inner.Exists() && inner.Type == gjson.String {
			return inner.String()
		}
		if inner := thinkingField.Get("thinking"); inner.Exists() && inner.Type == gjson.String {
			return inner.String()
		}
	}

	return ""
}
