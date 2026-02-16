package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// Block is a displayable piece of a message.
type Block struct {
	Type  string          `json:"type"` // "text" or "tool_use"
	Text  string          `json:"text"`
	Input json.RawMessage `json:"input,omitempty"` // tool_use input (preserved for select tools)
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
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // up to 10MB lines

	for scanner.Scan() {
		line := scanner.Bytes()
		msg, ok := parseLine(line)
		if ok {
			messages = append(messages, msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

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
}

// messageEnvelope is the message field inside a JSONL entry.
type messageEnvelope struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// contentBlock is a single block in the content array.
type contentBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`    // for tool_use
	Input   json.RawMessage `json:"input"`   // for tool_use
	Content any             `json:"content"` // for tool_result
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
			if b.Text != "" {
				hasNonToolResult = true
				displayBlocks = append(displayBlocks, Block{Type: "text", Text: b.Text})
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

	ts, _ := time.Parse(time.RFC3339Nano, entry.Timestamp)

	var blocks []contentBlock
	if err := json.Unmarshal(env.Content, &blocks); err != nil {
		return Message{}, false
	}

	var displayBlocks []Block
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				displayBlocks = append(displayBlocks, Block{Type: "text", Text: b.Text})
			}
		case "tool_use":
			blk := Block{Type: "tool_use", Text: b.Name}
			if toolsWithDisplayableInput[b.Name] && len(b.Input) > 0 {
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
