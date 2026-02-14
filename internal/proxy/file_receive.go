package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// FileReceiveInspector intercepts client→gateway chat.send messages that
// contain file attachments, saves the file content to disk, and rewrites
// the message to include FILE_RECEIVED markers so the AI agent can read
// the files directly from the filesystem.
//
// Only attachments with type "file" are processed; images (type "image")
// are left for the gateway's existing handling.
//
// Fail-open: any error during processing logs a warning and returns the
// original payload unchanged.
type FileReceiveInspector struct {
	InboxDir string
	Logger   *slog.Logger
}

func (f *FileReceiveInspector) InspectMessage(payload []byte, msgType websocket.MessageType) []byte {
	if msgType != websocket.MessageText {
		return payload
	}

	// Quick envelope check — only process chat.send requests.
	var env struct {
		Type   string `json:"type"`
		Method string `json:"method,omitempty"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return payload
	}
	if env.Type != "req" || env.Method != "chat.send" {
		return payload
	}

	// Parse the full message preserving unknown fields.
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		f.Logger.Warn("file receive: failed to parse message", "error", err)
		return payload
	}

	paramsRaw, ok := msg["params"]
	if !ok {
		return payload
	}

	var params map[string]json.RawMessage
	if err := json.Unmarshal(paramsRaw, &params); err != nil {
		f.Logger.Warn("file receive: failed to parse params", "error", err)
		return payload
	}

	attachmentsRaw, ok := params["attachments"]
	if !ok {
		return payload
	}

	var attachments []map[string]interface{}
	if err := json.Unmarshal(attachmentsRaw, &attachments); err != nil {
		f.Logger.Warn("file receive: failed to parse attachments", "error", err)
		return payload
	}

	// Process file attachments.
	var markers []string
	modified := false

	for i, att := range attachments {
		attType, _ := att["type"].(string)
		if attType != "file" {
			continue
		}

		contentStr, _ := att["content"].(string)
		if contentStr == "" {
			continue
		}

		fileName, _ := att["fileName"].(string)
		if fileName == "" {
			fileName = "unnamed_file"
		}

		mimeType, _ := att["mimeType"].(string)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}

		// Decode base64 content.
		data, err := base64.StdEncoding.DecodeString(contentStr)
		if err != nil {
			f.Logger.Warn("file receive: bad base64", "file", fileName, "error", err)
			continue
		}

		// Sanitize filename: strip path components.
		safeName := filepath.Base(fileName)
		safeName = strings.ReplaceAll(safeName, string(os.PathSeparator), "_")
		if safeName == "." || safeName == ".." {
			safeName = "unnamed_file"
		}

		// Handle filename collisions.
		destPath := filepath.Join(f.InboxDir, safeName)
		if _, err := os.Stat(destPath); err == nil {
			ext := filepath.Ext(safeName)
			base := strings.TrimSuffix(safeName, ext)
			safeName = fmt.Sprintf("%s_%d%s", base, time.Now().UnixMilli(), ext)
			destPath = filepath.Join(f.InboxDir, safeName)
		}

		// Atomic write: temp file then rename.
		tmpFile, err := os.CreateTemp(f.InboxDir, ".recv-*")
		if err != nil {
			f.Logger.Warn("file receive: failed to create temp file", "error", err)
			continue
		}
		tmpPath := tmpFile.Name()

		if _, err := tmpFile.Write(data); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			f.Logger.Warn("file receive: failed to write file", "file", safeName, "error", err)
			continue
		}
		tmpFile.Close()

		if err := os.Rename(tmpPath, destPath); err != nil {
			os.Remove(tmpPath)
			f.Logger.Warn("file receive: failed to rename file", "file", safeName, "error", err)
			continue
		}

		marker := fmt.Sprintf("FILE_RECEIVED: %s (%s, %d bytes)", destPath, mimeType, len(data))
		markers = append(markers, marker)

		// Strip base64 content from attachment to reduce payload size.
		delete(attachments[i], "content")
		modified = true

		f.Logger.Info("file saved", "path", destPath, "size", len(data), "mime", mimeType)
	}

	if !modified {
		return payload
	}

	// Append FILE_RECEIVED markers to the message text.
	var messageText string
	if raw, ok := params["message"]; ok {
		json.Unmarshal(raw, &messageText)
	}
	if messageText != "" {
		messageText += "\n"
	}
	messageText += strings.Join(markers, "\n")

	messageBytes, err := json.Marshal(messageText)
	if err != nil {
		f.Logger.Warn("file receive: failed to marshal message", "error", err)
		return payload
	}
	params["message"] = messageBytes

	// Re-marshal attachments back into params.
	attBytes, err := json.Marshal(attachments)
	if err != nil {
		f.Logger.Warn("file receive: failed to marshal attachments", "error", err)
		return payload
	}
	params["attachments"] = attBytes

	// Re-marshal params back into msg.
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		f.Logger.Warn("file receive: failed to marshal params", "error", err)
		return payload
	}
	msg["params"] = paramsBytes

	// Re-marshal the full message.
	result, err := json.Marshal(msg)
	if err != nil {
		f.Logger.Warn("file receive: failed to marshal message", "error", err)
		return payload
	}

	return result
}
