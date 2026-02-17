package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
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
