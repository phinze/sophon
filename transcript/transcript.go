package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// Block is a displayable piece of a message.
type Block struct {
	Type    string          `json:"type"` // "text" or "tool_use"
	Text    string          `json:"text"`
	Summary string          `json:"summary,omitempty"` // concise tool description
	Input   json.RawMessage `json:"input,omitempty"`   // tool_use input (preserved for select tools)

	toolUseID string          // for linking to tool_result during post-processing
	toolInput json.RawMessage // for summary generation
}

// Message is a single user or assistant turn.
type Message struct {
	Role      string    `json:"role"` // "user" or "assistant"
	Timestamp time.Time `json:"timestamp"`
	Blocks    []Block   `json:"blocks"`
}

// Transcript is a parsed conversation.
type Transcript struct {
	Messages []Message `json:"messages"`
}

// TranscriptPath returns the expected JSONL path for a given session.
// Claude Code stores transcripts at ~/.claude/projects/{cwd-slug}/{session-id}.jsonl
// where cwd-slug replaces all "/" with "-".
func TranscriptPath(claudeDir, cwd, sessionID string) string {
	slug := cwdToSlug(cwd)
	return claudeDir + "/projects/" + slug + "/" + sessionID + ".jsonl"
}

func cwdToSlug(cwd string) string {
	slug := strings.ReplaceAll(cwd, "/", "-")
	slug = strings.ReplaceAll(slug, ".", "-")
	return slug
}

// Read parses a Claude Code JSONL transcript file and returns displayable messages.
func Read(path string) (*Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var messages []Message
	toolResults := map[string]string{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // up to 10MB lines

	for scanner.Scan() {
		line := scanner.Bytes()
		collectToolResults(line, toolResults)
		msg, ok := parseLine(line)
		if ok {
			messages = append(messages, msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	attachSummaries(messages, toolResults)
	return &Transcript{Messages: messages}, nil
}

// LastAssistantText returns the last text block from the last assistant message,
// or "" if none found.
func LastAssistantText(t *Transcript) string {
	for i := len(t.Messages) - 1; i >= 0; i-- {
		if t.Messages[i].Role != "assistant" {
			continue
		}
		for j := len(t.Messages[i].Blocks) - 1; j >= 0; j-- {
			if t.Messages[i].Blocks[j].Type == "text" {
				return t.Messages[i].Blocks[j].Text
			}
		}
	}
	return ""
}

// jsonlEntry is the raw structure of a JSONL line.
type jsonlEntry struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
	IsMeta    bool            `json:"isMeta"`
}

// messageEnvelope is the message field inside a JSONL entry.
type messageEnvelope struct {
	Role              string          `json:"role"`
	Content           json.RawMessage `json:"content"`
	Model             string          `json:"model"`
	IsApiErrorMessage bool            `json:"isApiErrorMessage"`
}

// contentBlock is a single block in the content array.
type contentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`          // tool_use ID
	Text      string          `json:"text"`
	Name      string          `json:"name"`         // for tool_use
	Input     json.RawMessage `json:"input"`        // for tool_use
	ToolUseID string          `json:"tool_use_id"`  // for tool_result link
	Content   any             `json:"content"`      // for tool_result
}

// toolsWithDisplayableInput lists tool names whose Input should be preserved for display.
var toolsWithDisplayableInput = map[string]bool{
	"AskUserQuestion": true,
}

func parseLine(line []byte) (Message, bool) {
	var entry jsonlEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return Message{}, false
	}

	if entry.IsMeta {
		return Message{}, false
	}

	// Only process "user" and "assistant" type entries
	switch entry.Type {
	case "user":
		return parseUserEntry(entry)
	case "assistant":
		return parseAssistantEntry(entry)
	default:
		return Message{}, false
	}
}

func parseUserEntry(entry jsonlEntry) (Message, bool) {
	var env messageEnvelope
	if err := json.Unmarshal(entry.Message, &env); err != nil {
		return Message{}, false
	}
	if env.Role != "user" {
		return Message{}, false
	}

	ts, _ := time.Parse(time.RFC3339Nano, entry.Timestamp)

	// Content can be a string or an array
	// Try string first
	var strContent string
	if err := json.Unmarshal(env.Content, &strContent); err == nil {
		strContent = stripSystemReminders(strContent)
		if strContent == "" {
			return Message{}, false
		}
		return Message{
			Role:      "user",
			Timestamp: ts,
			Blocks:    []Block{{Type: "text", Text: strContent}},
		}, true
	}

	// Try array of content blocks
	var blocks []contentBlock
	if err := json.Unmarshal(env.Content, &blocks); err != nil {
		return Message{}, false
	}

	// Check if this is only tool_result blocks (automatic feedback, skip)
	hasNonToolResult := false
	var displayBlocks []Block
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text := stripSystemReminders(b.Text)
			if text != "" {
				hasNonToolResult = true
				displayBlocks = append(displayBlocks, Block{Type: "text", Text: text})
			}
		case "tool_result":
			// skip — automatic feedback
		default:
			// skip unknown
		}
	}

	if !hasNonToolResult || len(displayBlocks) == 0 {
		return Message{}, false
	}

	return Message{
		Role:      "user",
		Timestamp: ts,
		Blocks:    displayBlocks,
	}, true
}

func parseAssistantEntry(entry jsonlEntry) (Message, bool) {
	var env messageEnvelope
	if err := json.Unmarshal(entry.Message, &env); err != nil {
		return Message{}, false
	}
	if env.Role != "assistant" {
		return Message{}, false
	}
	if env.IsApiErrorMessage || env.Model == "<synthetic>" {
		return Message{}, false
	}

	ts, _ := time.Parse(time.RFC3339Nano, entry.Timestamp)

	var blocks []contentBlock
	if err := json.Unmarshal(env.Content, &blocks); err != nil {
		return Message{}, false
	}

	// Check if this message contains ExitPlanMode — if so, preserve the
	// Write tool input so the plan content can be displayed.
	hasExitPlanMode := false
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name == "ExitPlanMode" {
			hasExitPlanMode = true
			break
		}
	}

	var displayBlocks []Block
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text := stripSystemReminders(b.Text)
			if text != "" {
				displayBlocks = append(displayBlocks, Block{Type: "text", Text: text})
			}
		case "tool_use":
			blk := Block{
				Type:      "tool_use",
				Text:      b.Name,
				toolUseID: b.ID,
				toolInput: b.Input,
			}
			if toolsWithDisplayableInput[b.Name] && len(b.Input) > 0 {
				blk.Input = b.Input
			} else if hasExitPlanMode && b.Name == "Write" && len(b.Input) > 0 {
				blk.Input = b.Input
			}
			displayBlocks = append(displayBlocks, blk)
		case "thinking":
			// skip
		default:
			// skip
		}
	}

	if len(displayBlocks) == 0 {
		return Message{}, false
	}

	return Message{
		Role:      "assistant",
		Timestamp: ts,
		Blocks:    displayBlocks,
	}, true
}

var systemReminderRe = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

func stripSystemReminders(s string) string {
	return strings.TrimSpace(systemReminderRe.ReplaceAllString(s, ""))
}

// collectToolResults extracts tool_result text from a JSONL line (including isMeta entries)
// and adds them to the results map keyed by tool_use_id.
func collectToolResults(line []byte, results map[string]string) {
	var entry jsonlEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return
	}

	// Only user entries contain tool_result blocks
	if entry.Type != "user" {
		return
	}

	var env messageEnvelope
	if err := json.Unmarshal(entry.Message, &env); err != nil {
		return
	}
	if env.Role != "user" {
		return
	}

	var blocks []contentBlock
	if err := json.Unmarshal(env.Content, &blocks); err != nil {
		return
	}

	for _, b := range blocks {
		if b.Type == "tool_result" && b.ToolUseID != "" {
			results[b.ToolUseID] = extractResultText(b.Content)
		}
	}
}

// extractResultText pulls text from a tool_result content field.
// Content can be a string or an array of {type:"text", text:"..."} blocks.
func extractResultText(content any) string {
	if content == nil {
		return ""
	}
	// String content
	if s, ok := content.(string); ok {
		return s
	}
	// Array content: [{type:"text", text:"..."}]
	if arr, ok := content.([]any); ok {
		var parts []string
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// attachSummaries generates summary strings for tool_use blocks.
func attachSummaries(messages []Message, toolResults map[string]string) {
	for i := range messages {
		for j := range messages[i].Blocks {
			blk := &messages[i].Blocks[j]
			if blk.Type != "tool_use" {
				continue
			}
			summary := summarizeTool(blk.Text, blk.toolInput)
			// Check for error in result
			if result, ok := toolResults[blk.toolUseID]; ok {
				if strings.Contains(result, "<tool_use_error>") {
					summary += " (error)"
				}
			}
			blk.Summary = summary
		}
	}
}

// summarizeTool generates a concise summary for a tool_use block based on name and input.
func summarizeTool(name string, input json.RawMessage) string {
	var fields map[string]json.RawMessage
	if len(input) > 0 {
		json.Unmarshal(input, &fields) //nolint: errcheck
	}

	getString := func(key string) string {
		raw, ok := fields[key]
		if !ok {
			return ""
		}
		var s string
		json.Unmarshal(raw, &s) //nolint: errcheck
		return s
	}

	switch name {
	case "Read":
		if p := getString("file_path"); p != "" {
			return "Read " + shortenPath(p)
		}
	case "Bash":
		if cmd := getString("command"); cmd != "" {
			return "Bash: " + truncate(cmd, 50)
		}
	case "Edit":
		if p := getString("file_path"); p != "" {
			return "Edit " + shortenPath(p)
		}
	case "Write":
		if p := getString("file_path"); p != "" {
			return "Write " + shortenPath(p)
		}
	case "Grep":
		if pat := getString("pattern"); pat != "" {
			return fmt.Sprintf("Grep \u00ab%s\u00bb", truncate(pat, 40))
		}
	case "Glob":
		if pat := getString("pattern"); pat != "" {
			return "Glob " + truncate(pat, 40)
		}
	case "Task":
		if desc := getString("description"); desc != "" {
			return "Task: " + truncate(desc, 50)
		}
	case "WebSearch":
		if q := getString("query"); q != "" {
			return fmt.Sprintf("WebSearch \u00ab%s\u00bb", truncate(q, 40))
		}
	case "WebFetch":
		if u := getString("url"); u != "" {
			return "WebFetch " + truncate(u, 50)
		}
	}

	// MCP tools: mcp__server__toolname → "toolname: first_arg"
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.SplitN(name, "__", 3)
		if len(parts) == 3 {
			toolName := parts[2]
			// Try to find a recognizable input field
			for _, key := range []string{"query", "id", "name", "title", "issueId", "team"} {
				if v := getString(key); v != "" {
					return toolName + ": " + truncate(v, 40)
				}
			}
			return toolName
		}
	}

	return name
}

// shortenPath returns the last 2-3 components of a path, capped at 40 chars.
func shortenPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 3 {
		return truncate(p, 40)
	}
	short := strings.Join(parts[len(parts)-3:], "/")
	return truncate(short, 40)
}

// truncate shortens s to max chars, adding "..." if truncated.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
