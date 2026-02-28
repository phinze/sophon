package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCwdToSlug(t *testing.T) {
	tests := []struct {
		cwd  string
		want string
	}{
		{"/home/phinze/foo", "-home-phinze-foo"},
		{"/", "-"},
		{"/a/b/c", "-a-b-c"},
		{"/home/phinze/src/github.com/phinze/sophon", "-home-phinze-src-github-com-phinze-sophon"},
	}
	for _, tt := range tests {
		got := cwdToSlug(tt.cwd)
		if got != tt.want {
			t.Errorf("cwdToSlug(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}

func TestTranscriptPath(t *testing.T) {
	got := TranscriptPath("/home/user/.claude", "/home/user/src/github.com/org/project", "abc-123")
	want := "/home/user/.claude/projects/-home-user-src-github-com-org-project/abc-123.jsonl"
	if got != want {
		t.Errorf("TranscriptPath = %q, want %q", got, want)
	}
}

func TestReadFileNotFound(t *testing.T) {
	_, err := Read("/nonexistent/file.jsonl")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestReadUserStringContent(t *testing.T) {
	jsonl := `{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":"Hello there"}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	m := tr.Messages[0]
	if m.Role != "user" {
		t.Errorf("role = %q, want user", m.Role)
	}
	if len(m.Blocks) != 1 || m.Blocks[0].Text != "Hello there" {
		t.Errorf("blocks = %+v", m.Blocks)
	}
}

func TestReadUserArrayContent(t *testing.T) {
	jsonl := `{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"What is this?"}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	if tr.Messages[0].Blocks[0].Text != "What is this?" {
		t.Errorf("text = %q", tr.Messages[0].Blocks[0].Text)
	}
}

func TestReadSkipsToolResultOnlyUser(t *testing.T) {
	jsonl := `{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"123","content":"ok"}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 0 {
		t.Fatalf("expected 0 messages for tool_result-only user, got %d", len(tr.Messages))
	}
}

func TestReadAssistantTextAndToolUse(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Let me check."},{"type":"tool_use","id":"t1","name":"Read","input":{}}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	m := tr.Messages[0]
	if m.Role != "assistant" {
		t.Errorf("role = %q", m.Role)
	}
	if len(m.Blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(m.Blocks))
	}
	if m.Blocks[0].Type != "text" || m.Blocks[0].Text != "Let me check." {
		t.Errorf("block 0 = %+v", m.Blocks[0])
	}
	if m.Blocks[1].Type != "tool_use" || m.Blocks[1].Text != "Read" {
		t.Errorf("block 1 = %+v", m.Blocks[1])
	}
}

func TestReadSkipsThinking(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"Here you go."}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	if len(tr.Messages[0].Blocks) != 1 {
		t.Fatalf("got %d blocks, want 1 (thinking should be skipped)", len(tr.Messages[0].Blocks))
	}
	if tr.Messages[0].Blocks[0].Text != "Here you go." {
		t.Errorf("text = %q", tr.Messages[0].Blocks[0].Text)
	}
}

func TestReadSkipsProgressAndOtherTypes(t *testing.T) {
	lines := `{"type":"progress","data":{"type":"hook_progress"}}
{"type":"file-history-snapshot","snapshot":{}}
{"type":"system","message":"hello"}
{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":"real message"}}
`
	tr := readFromString(t, lines)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1 (only user message)", len(tr.Messages))
	}
}

func TestLastAssistantText(t *testing.T) {
	tr := &Transcript{
		Messages: []Message{
			{Role: "user", Blocks: []Block{{Type: "text", Text: "hi"}}},
			{Role: "assistant", Blocks: []Block{{Type: "text", Text: "first reply"}}},
			{Role: "user", Blocks: []Block{{Type: "text", Text: "thanks"}}},
			{Role: "assistant", Blocks: []Block{
				{Type: "tool_use", Text: "Bash"},
				{Type: "text", Text: "last reply"},
			}},
		},
	}
	got := LastAssistantText(tr)
	if got != "last reply" {
		t.Errorf("LastAssistantText = %q, want %q", got, "last reply")
	}
}

func TestLastAssistantTextEmpty(t *testing.T) {
	tr := &Transcript{Messages: []Message{
		{Role: "user", Blocks: []Block{{Type: "text", Text: "hi"}}},
	}}
	got := LastAssistantText(tr)
	if got != "" {
		t.Errorf("LastAssistantText = %q, want empty", got)
	}
}

func TestLastAssistantTextToolUseOnly(t *testing.T) {
	tr := &Transcript{Messages: []Message{
		{Role: "assistant", Blocks: []Block{{Type: "tool_use", Text: "Bash"}}},
	}}
	got := LastAssistantText(tr)
	if got != "" {
		t.Errorf("LastAssistantText = %q, want empty (no text blocks)", got)
	}
}

func TestReadMixedConversation(t *testing.T) {
	lines := `{"type":"file-history-snapshot","snapshot":{}}
{"type":"progress","data":{"type":"hook_progress"}}
{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":"Fix the bug"}}
{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Let me think..."},{"type":"text","text":"I'll look into it."}]}}
{"type":"assistant","timestamp":"2026-01-01T00:00:02.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/foo"}}]}}
{"type":"user","timestamp":"2026-01-01T00:00:03.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"file contents"}]}}
{"type":"assistant","timestamp":"2026-01-01T00:00:04.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Found the issue."}]}}
`
	tr := readFromString(t, lines)
	// Expected: user("Fix the bug"), assistant("I'll look into it."), assistant(tool_use:Read), assistant("Found the issue.")
	// The tool_result user message should be skipped
	if len(tr.Messages) != 4 {
		t.Fatalf("got %d messages, want 4", len(tr.Messages))
	}
	if tr.Messages[0].Role != "user" || tr.Messages[0].Blocks[0].Text != "Fix the bug" {
		t.Errorf("msg 0: %+v", tr.Messages[0])
	}
	if tr.Messages[1].Role != "assistant" || tr.Messages[1].Blocks[0].Text != "I'll look into it." {
		t.Errorf("msg 1: %+v", tr.Messages[1])
	}
	if tr.Messages[2].Role != "assistant" || tr.Messages[2].Blocks[0].Type != "tool_use" {
		t.Errorf("msg 2: %+v", tr.Messages[2])
	}
	if tr.Messages[3].Role != "assistant" || tr.Messages[3].Blocks[0].Text != "Found the issue." {
		t.Errorf("msg 3: %+v", tr.Messages[3])
	}
}

func TestReadAskUserQuestionPreservesInput(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"I have a question."},{"type":"tool_use","id":"t1","name":"AskUserQuestion","input":{"questions":[{"question":"Which approach?","header":"Approach","options":[{"label":"Option A","description":"First option"},{"label":"Option B","description":"Second option"}],"multiSelect":false}]}}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	m := tr.Messages[0]
	if len(m.Blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(m.Blocks))
	}
	if m.Blocks[1].Type != "tool_use" || m.Blocks[1].Text != "AskUserQuestion" {
		t.Errorf("block 1 = %+v", m.Blocks[1])
	}
	if m.Blocks[1].Input == nil {
		t.Fatal("AskUserQuestion input should be preserved")
	}
	// Verify the input contains the question data
	var input map[string]interface{}
	if err := json.Unmarshal(m.Blocks[1].Input, &input); err != nil {
		t.Fatalf("failed to parse input: %v", err)
	}
	questions, ok := input["questions"].([]interface{})
	if !ok || len(questions) != 1 {
		t.Errorf("expected 1 question, got %v", input["questions"])
	}
}

func TestReadRegularToolUseOmitsInput(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/foo"}}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	if tr.Messages[0].Blocks[0].Input != nil {
		t.Error("regular tool_use should not have input preserved")
	}
}

func TestReadExitPlanModePreservesWriteInput(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Here is the plan."},{"type":"tool_use","id":"t1","name":"Write","input":{"file_path":"/tmp/plan.md","content":"## Plan\n\nDo the thing."}},{"type":"tool_use","id":"t2","name":"ExitPlanMode","input":{}}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	m := tr.Messages[0]
	if len(m.Blocks) != 3 {
		t.Fatalf("got %d blocks, want 3", len(m.Blocks))
	}

	// Write input should be preserved because ExitPlanMode is present
	writeBlock := m.Blocks[1]
	if writeBlock.Text != "Write" {
		t.Errorf("block 1 text = %q, want Write", writeBlock.Text)
	}
	if writeBlock.Input == nil {
		t.Fatal("Write input should be preserved when ExitPlanMode is present")
	}
	var writeInput map[string]interface{}
	if err := json.Unmarshal(writeBlock.Input, &writeInput); err != nil {
		t.Fatalf("failed to parse Write input: %v", err)
	}
	if writeInput["content"] != "## Plan\n\nDo the thing." {
		t.Errorf("Write content = %v", writeInput["content"])
	}

	// ExitPlanMode block should be present
	if m.Blocks[2].Text != "ExitPlanMode" {
		t.Errorf("block 2 text = %q, want ExitPlanMode", m.Blocks[2].Text)
	}
}

func TestReadExitPlanModePreservesWriteInputCrossMessage(t *testing.T) {
	// Write tool and ExitPlanMode in different assistant messages (common case:
	// Claude writes the plan file, gets the result, then calls ExitPlanMode).
	lines := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Here is the plan."},{"type":"tool_use","id":"t1","name":"Write","input":{"file_path":"/tmp/plan.md","content":"## Plan\n\nStep 1: Do the thing."}}]}}
{"type":"user","timestamp":"2026-01-01T00:00:02.000Z","isMeta":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
{"type":"assistant","timestamp":"2026-01-01T00:00:03.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"ExitPlanMode","input":{}}]}}
`
	tr := readFromString(t, lines)
	// Expected: 2 assistant messages (tool_result user is filtered)
	if len(tr.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(tr.Messages))
	}

	// Write input should be preserved even though ExitPlanMode is in a different message
	writeBlock := tr.Messages[0].Blocks[1]
	if writeBlock.Text != "Write" {
		t.Errorf("block 1 text = %q, want Write", writeBlock.Text)
	}
	if writeBlock.Input == nil {
		t.Fatal("Write input should be preserved when ExitPlanMode is in a subsequent message")
	}
	var writeInput map[string]interface{}
	if err := json.Unmarshal(writeBlock.Input, &writeInput); err != nil {
		t.Fatalf("failed to parse Write input: %v", err)
	}
	if writeInput["content"] != "## Plan\n\nStep 1: Do the thing." {
		t.Errorf("Write content = %v", writeInput["content"])
	}

	// ExitPlanMode block should be present in second message
	if tr.Messages[1].Blocks[0].Text != "ExitPlanMode" {
		t.Errorf("msg 1 block 0 text = %q, want ExitPlanMode", tr.Messages[1].Blocks[0].Text)
	}
}

func TestReadWriteWithoutExitPlanModeOmitsInput(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Write","input":{"file_path":"/tmp/foo.go","content":"package main"}}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	if tr.Messages[0].Blocks[0].Input != nil {
		t.Error("Write input should not be preserved without ExitPlanMode")
	}
}

func TestReadSkipsIsMetaUser(t *testing.T) {
	jsonl := `{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>something</local-command-caveat>"}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 0 {
		t.Fatalf("expected 0 messages for isMeta user, got %d", len(tr.Messages))
	}
}

func TestReadSkipsSyntheticApiError(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","model":"<synthetic>","isApiErrorMessage":true,"content":[{"type":"text","text":"API error occurred"}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 0 {
		t.Fatalf("expected 0 messages for synthetic API error, got %d", len(tr.Messages))
	}
}

func TestReadSkipsSyntheticModelOnly(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","model":"<synthetic>","content":[{"type":"text","text":"Some injected text"}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 0 {
		t.Fatalf("expected 0 messages for synthetic model, got %d", len(tr.Messages))
	}
}

func TestReadStripsSystemRemindersFromText(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Real content.\n<system-reminder>injected noise</system-reminder>"}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	if tr.Messages[0].Blocks[0].Text != "Real content." {
		t.Errorf("text = %q, want %q", tr.Messages[0].Blocks[0].Text, "Real content.")
	}
}

func TestReadStripsSystemRemindersFromUserString(t *testing.T) {
	jsonl := `{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":"Hello\n<system-reminder>noise</system-reminder>\nWorld"}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	if tr.Messages[0].Blocks[0].Text != "Hello\n\nWorld" {
		t.Errorf("text = %q, want %q", tr.Messages[0].Blocks[0].Text, "Hello\n\nWorld")
	}
}

func TestReadSystemReminderOnlyTextDropsMessage(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"<system-reminder>only noise here</system-reminder>"}]}}` + "\n"

	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 0 {
		t.Fatalf("expected 0 messages when content is only system-reminder, got %d", len(tr.Messages))
	}
}

func TestReadMixedConversationWithNoise(t *testing.T) {
	lines := `{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>caveat</local-command-caveat>"}}
{"type":"user","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":"Fix the bug"}}
{"type":"assistant","timestamp":"2026-01-01T00:00:02.000Z","message":{"role":"assistant","model":"<synthetic>","isApiErrorMessage":true,"content":[{"type":"text","text":"error msg"}]}}
{"type":"assistant","timestamp":"2026-01-01T00:00:03.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Sure, let me look.\n<system-reminder>injected</system-reminder>"}]}}
{"type":"assistant","timestamp":"2026-01-01T00:00:04.000Z","message":{"role":"assistant","content":[{"type":"text","text":"<system-reminder>only noise</system-reminder>"}]}}
{"type":"assistant","timestamp":"2026-01-01T00:00:05.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Done!"}]}}
`
	tr := readFromString(t, lines)
	// Expected: user("Fix the bug"), assistant("Sure, let me look."), assistant("Done!")
	// Filtered: isMeta user, synthetic API error, system-reminder-only assistant
	if len(tr.Messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(tr.Messages))
	}
	if tr.Messages[0].Role != "user" || tr.Messages[0].Blocks[0].Text != "Fix the bug" {
		t.Errorf("msg 0: %+v", tr.Messages[0])
	}
	if tr.Messages[1].Role != "assistant" || tr.Messages[1].Blocks[0].Text != "Sure, let me look." {
		t.Errorf("msg 1: %+v", tr.Messages[1])
	}
	if tr.Messages[2].Role != "assistant" || tr.Messages[2].Blocks[0].Text != "Done!" {
		t.Errorf("msg 2: %+v", tr.Messages[2])
	}
}

// readFromString writes content to a temp file and reads it as a transcript.
func readFromString(t *testing.T, content string) *Transcript {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	tr, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return tr
}

// --- Tool summary tests ---

func TestToolSummaryRead(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/home/user/src/main.go"}}]}}
{"type":"user","timestamp":"2026-01-01T00:00:02.000Z","isMeta":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"package main"}]}}
`
	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	blk := tr.Messages[0].Blocks[0]
	if blk.Summary != "Read user/src/main.go" {
		t.Errorf("summary = %q, want %q", blk.Summary, "Read user/src/main.go")
	}
}

func TestToolSummaryBash(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}]}}
{"type":"user","timestamp":"2026-01-01T00:00:02.000Z","isMeta":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"PASS"}]}}
`
	tr := readFromString(t, jsonl)
	blk := tr.Messages[0].Blocks[0]
	if blk.Summary != "Bash: go test ./..." {
		t.Errorf("summary = %q, want %q", blk.Summary, "Bash: go test ./...")
	}
}

func TestToolSummaryEdit(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/home/user/src/transcript.go","old_string":"foo","new_string":"bar"}}]}}
`
	tr := readFromString(t, jsonl)
	blk := tr.Messages[0].Blocks[0]
	if blk.Summary != "Edit user/src/transcript.go" {
		t.Errorf("summary = %q, want %q", blk.Summary, "Edit user/src/transcript.go")
	}
}

func TestToolSummaryGrep(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Grep","input":{"pattern":"parseUser"}}]}}
`
	tr := readFromString(t, jsonl)
	blk := tr.Messages[0].Blocks[0]
	want := "Grep \u00abparseUser\u00bb"
	if blk.Summary != want {
		t.Errorf("summary = %q, want %q", blk.Summary, want)
	}
}

func TestToolSummaryMCP(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"mcp__linear-server__get_issue","input":{"id":"LIN-123"}}]}}
`
	tr := readFromString(t, jsonl)
	blk := tr.Messages[0].Blocks[0]
	if blk.Summary != "get_issue: LIN-123" {
		t.Errorf("summary = %q, want %q", blk.Summary, "get_issue: LIN-123")
	}
}

func TestToolSummaryLongCommand(t *testing.T) {
	longCmd := "go test -v -count=1 -run TestSomethingVeryLongNameHere ./pkg/something/deeply/nested/..."
	input, _ := json.Marshal(map[string]string{"command": longCmd})
	entry := map[string]any{
		"type":      "assistant",
		"timestamp": "2026-01-01T00:00:01.000Z",
		"message": map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "tool_use", "id": "t1", "name": "Bash", "input": json.RawMessage(input)},
			},
		},
	}
	line, _ := json.Marshal(entry)
	tr := readFromString(t, string(line)+"\n")
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	blk := tr.Messages[0].Blocks[0]
	if len(blk.Summary) > 60 { // "Bash: " + 50 + "..."
		t.Errorf("summary too long: %d chars: %q", len(blk.Summary), blk.Summary)
	}
	if !strings.HasSuffix(blk.Summary, "...") {
		t.Errorf("expected truncated summary ending with ..., got %q", blk.Summary)
	}
}

func TestToolSummaryError(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"false"}}]}}
{"type":"user","timestamp":"2026-01-01T00:00:02.000Z","isMeta":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"<tool_use_error>command failed</tool_use_error>"}]}}
`
	tr := readFromString(t, jsonl)
	blk := tr.Messages[0].Blocks[0]
	if !strings.HasSuffix(blk.Summary, " (error)") {
		t.Errorf("expected error suffix, got summary = %q", blk.Summary)
	}
}

func TestToolSummaryNoResult(t *testing.T) {
	// Tool use without any corresponding result — should still get input-based summary
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/tmp/foo.go"}}]}}
`
	tr := readFromString(t, jsonl)
	blk := tr.Messages[0].Blocks[0]
	if blk.Summary != "Read /tmp/foo.go" {
		t.Errorf("summary = %q, want %q", blk.Summary, "Read /tmp/foo.go")
	}
}

func TestToolSummaryFallback(t *testing.T) {
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"UnknownTool","input":{}}]}}
`
	tr := readFromString(t, jsonl)
	blk := tr.Messages[0].Blocks[0]
	if blk.Summary != "UnknownTool" {
		t.Errorf("summary = %q, want %q", blk.Summary, "UnknownTool")
	}
}

func TestToolSummaryLinkedFromIsMeta(t *testing.T) {
	// The tool_result is in an isMeta entry (which parseLine filters out)
	// but collectToolResults should still extract it
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"echo hello"}}]}}
{"type":"user","timestamp":"2026-01-01T00:00:02.000Z","isMeta":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"<tool_use_error>permission denied</tool_use_error>"}]}}
`
	tr := readFromString(t, jsonl)
	if len(tr.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(tr.Messages))
	}
	blk := tr.Messages[0].Blocks[0]
	if blk.Summary != "Bash: echo hello (error)" {
		t.Errorf("summary = %q, want %q", blk.Summary, "Bash: echo hello (error)")
	}
}

func TestShortenPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/tmp/foo.go", "/tmp/foo.go"},
		{"/a/b/c", "a/b/c"},
		{"/home/user/src/github.com/org/project/main.go", "org/project/main.go"},
	}
	for _, tt := range tests {
		got := shortenPath(tt.input)
		if got != tt.want {
			t.Errorf("shortenPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("a very long string that exceeds", 10); got != "a very lon..." {
		t.Errorf("truncate long = %q", got)
	}
}

func TestToolSummaryArrayContent(t *testing.T) {
	// tool_result with array content format
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","timestamp":"2026-01-01T00:00:02.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"<tool_use_error>not found</tool_use_error>"}]}]}}
`
	tr := readFromString(t, jsonl)
	blk := tr.Messages[0].Blocks[0]
	if !strings.HasSuffix(blk.Summary, " (error)") {
		t.Errorf("expected error suffix for array content, got summary = %q", blk.Summary)
	}
}
