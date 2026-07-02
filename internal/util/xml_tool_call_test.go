package util

import (
	"strings"
	"testing"
)

func TestParseXMLToolCalls_EditInvoke(t *testing.T) {
	content := `Để em refactor parser sang map[string]any:<tool_call>
<invoke name="Edit"><replace_all>false</replace_all><file_path>/home/vincent/dev/golang/tao-anh-server/backend/internal/oauth/antigravity/auth.go</file_path><old_string>var payload struct {
}</old_string><new_string>var payload map[string]any
}</new_string></invoke>
</tool_call>`

	prose := ProseBeforeXMLToolCalls(content)
	if !strings.HasPrefix(prose, "Để em refactor") {
		t.Fatalf("unexpected prose prefix: %q", prose)
	}
	if strings.Contains(prose, "<tool_call") {
		t.Fatalf("prose should not include tool_call markup: %q", prose)
	}

	invokes := ParseXMLToolCalls(content)
	if len(invokes) != 1 {
		t.Fatalf("expected 1 invoke, got %d", len(invokes))
	}
	if invokes[0].Name != "Edit" {
		t.Fatalf("expected tool name Edit, got %q", invokes[0].Name)
	}
	if invokes[0].Args["replace_all"] != false {
		t.Fatalf("expected replace_all=false, got %v", invokes[0].Args["replace_all"])
	}
	if invokes[0].Args["file_path"] == "" {
		t.Fatal("expected file_path argument")
	}
	if !strings.Contains(invokes[0].Args["old_string"].(string), "var payload struct") {
		t.Fatalf("unexpected old_string: %v", invokes[0].Args["old_string"])
	}

	argsJSON := invokes[0].ArgumentsJSON()
	if !strings.Contains(argsJSON, `"replace_all":false`) {
		t.Fatalf("expected boolean false in JSON, got %s", argsJSON)
	}
}

func TestParseXMLToolCalls_NoMarkup(t *testing.T) {
	if got := ParseXMLToolCalls("plain assistant text"); len(got) != 0 {
		t.Fatalf("expected no invokes, got %v", got)
	}
}

func TestParseXMLToolCalls_TruncatedInvoke(t *testing.T) {
	content := `Em thay parser sang map[string]any:<tool_call>
<invoke name="Edit"><replace_all>false</replace_all><file_path>/tmp/auth.go</file_path><old_string>var payload struct {
}</old_string><new_string>var payload map[string]any
// truncated before closing new_string`

	invokes := ParseXMLToolCalls(content)
	if len(invokes) != 1 {
		t.Fatalf("expected 1 invoke from truncated XML, got %d", len(invokes))
	}
	if invokes[0].Name != "Edit" {
		t.Fatalf("expected Edit, got %q", invokes[0].Name)
	}
	if invokes[0].Args["file_path"] != "/tmp/auth.go" {
		t.Fatalf("expected file_path, got %v", invokes[0].Args["file_path"])
	}
	if _, ok := invokes[0].Args["new_string"]; !ok {
		t.Fatal("expected partial new_string from truncated stream")
	}
}