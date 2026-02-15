package media

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cortexuvula/clawreachbridge/internal/config"
)

// mediaPathRe matches "MEDIA: /path/to/file.ext" lines in message text.
var mediaPathRe = regexp.MustCompile(`(?m)^MEDIA:\s*(/\S+)$`)

// Injector tracks chat runs and injects images from the gateway's media
// directory into final chat messages before they reach the client.
type Injector struct {
	cfg         config.MediaConfig
	allowedDirs []string // resolved absolute paths for MEDIA: path validation
	mu          sync.Mutex
	runStarts   map[string]time.Time // runId → first delta timestamp
}

// NewInjector creates a media injector with the given config.
func NewInjector(cfg config.MediaConfig) *Injector {
	dirs := cfg.AllowedDirs
	if len(dirs) == 0 && cfg.Directory != "" {
		dirs = []string{cfg.Directory}
	}
	// Resolve all allowed directories to absolute paths.
	var resolved []string
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil {
			slog.Warn("media: failed to resolve allowed_dir", "dir", d, "error", err)
			continue
		}
		// Ensure trailing separator for prefix matching.
		if !strings.HasSuffix(abs, string(filepath.Separator)) {
			abs += string(filepath.Separator)
		}
		resolved = append(resolved, abs)
	}
	if len(resolved) > 0 {
		slog.Info("media: path allowlist configured", "allowed_dirs", resolved)
	}
	return &Injector{
		cfg:         cfg,
		allowedDirs: resolved,
		runStarts:   make(map[string]time.Time),
	}
}

// outerMessage is the top-level WebSocket message envelope.
type outerMessage struct {
	Type    string          `json:"type"`
	Event   string          `json:"event,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// chatPayload is the chat event payload from the gateway.
type chatPayload struct {
	RunID      string          `json:"runId"`
	SessionKey string          `json:"sessionKey,omitempty"`
	Seq        int             `json:"seq,omitempty"`
	State      string          `json:"state"`
	Message    json.RawMessage `json:"message,omitempty"`
}

// chatMessage is the message object within a chat payload.
type chatMessage struct {
	Role      string        `json:"role"`
	Content   []contentItem `json:"content"`
	Timestamp int64         `json:"timestamp,omitempty"`
}

// contentItem is a single content element (text, image, or file).
type contentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Content  string `json:"content,omitempty"`
	FileName string `json:"fileName,omitempty"`
	FileSize int64  `json:"fileSize,omitempty"`
}

// ProcessMessage inspects a gateway→client WebSocket message and enriches
// chat final messages with images from the media directory. Non-chat messages
// and delta messages are returned unchanged.
func (inj *Injector) ProcessMessage(payload []byte) []byte {
	var outer outerMessage
	if err := json.Unmarshal(payload, &outer); err != nil {
		return payload
	}

	if outer.Type != "event" || outer.Event != "chat" {
		slog.Debug("media: skipping non-chat message", "type", outer.Type, "event", outer.Event)
		return payload
	}

	var chat chatPayload
	if err := json.Unmarshal(outer.Payload, &chat); err != nil {
		slog.Warn("media: failed to parse chat payload", "error", err)
		return payload
	}

	switch chat.State {
	case "delta":
		inj.trackDelta(chat.RunID)
		return inj.stripMediaFromDelta(payload, &outer, &chat)
	case "final":
		enriched, err := inj.enrichFinal(&outer, &chat)
		if err != nil {
			slog.Warn("media: failed to enrich final message", "error", err)
			return payload
		}
		return enriched
	default:
		slog.Debug("media: skipping chat message with unknown state", "state", chat.State, "runId", chat.RunID)
		return payload
	}
}

// trackDelta records the first-seen time for a runId.
func (inj *Injector) trackDelta(runID string) {
	if runID == "" {
		return
	}
	inj.mu.Lock()
	defer inj.mu.Unlock()

	if _, exists := inj.runStarts[runID]; !exists {
		inj.runStarts[runID] = time.Now()
		slog.Debug("media: tracking run", "runId", runID)
	}

	// Clean up stale entries to prevent memory leaks
	inj.cleanStaleLocked()
}

// cleanStaleLocked removes run entries older than MaxAge. Must be called with mu held.
func (inj *Injector) cleanStaleLocked() {
	cutoff := time.Now().Add(-inj.cfg.MaxAge * 2)
	for id, t := range inj.runStarts {
		if t.Before(cutoff) {
			delete(inj.runStarts, id)
			slog.Debug("media: cleaned stale run", "runId", id)
		}
	}
}

// stripMediaFromDelta removes MEDIA: lines from streaming delta messages so
// internal file paths don't flash on screen before the final message.
func (inj *Injector) stripMediaFromDelta(original []byte, outer *outerMessage, chat *chatPayload) []byte {
	if chat.Message == nil {
		return original
	}

	var msg chatMessage
	if err := json.Unmarshal(chat.Message, &msg); err != nil {
		return original
	}

	modified := false
	for i, ci := range msg.Content {
		if ci.Type != "text" {
			continue
		}
		cleaned := mediaPathRe.ReplaceAllString(ci.Text, "")
		cleaned = strings.TrimRight(cleaned, "\n")
		if cleaned != ci.Text {
			msg.Content[i].Text = cleaned
			modified = true
		}
	}

	if !modified {
		return original
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return original
	}
	chat.Message = msgBytes

	payloadBytes, err := json.Marshal(chat)
	if err != nil {
		return original
	}
	outer.Payload = payloadBytes

	result, err := json.Marshal(outer)
	if err != nil {
		return original
	}
	slog.Debug("media: stripped MEDIA markers from delta", "runId", chat.RunID)
	return result
}

// isPathAllowed checks that the resolved file path falls within one of the
// configured allowed directories. Resolves symlinks to prevent traversal.
func (inj *Injector) isPathAllowed(filePath string) bool {
	if len(inj.allowedDirs) == 0 {
		return true // no restriction configured
	}

	// Resolve symlinks and get absolute path.
	resolved, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		// File might not exist yet; fall back to Abs.
		resolved, err = filepath.Abs(filePath)
		if err != nil {
			return false
		}
	}

	for _, dir := range inj.allowedDirs {
		if strings.HasPrefix(resolved+string(filepath.Separator), dir) || strings.HasPrefix(resolved, dir) {
			return true
		}
	}
	return false
}

// enrichFinal extracts images from the message and injects them as content items.
// It first looks for explicit MEDIA: /path markers in the message text, then
// falls back to scanning the configured media directory for recent images.
func (inj *Injector) enrichFinal(outer *outerMessage, chat *chatPayload) ([]byte, error) {
	// Look up when this run started
	// Always use MaxAge window for directory scan instead of tracked runStart.
	// This ensures files created before the current run (e.g. in a prior tool-use
	// step) are still picked up, and avoids a race where multiple connections
	// processing the same final would consume the tracked start inconsistently.
	runStart := time.Now().Add(-inj.cfg.MaxAge)

	// Parse the message
	var msg chatMessage
	if err := json.Unmarshal(chat.Message, &msg); err != nil {
		return nil, fmt.Errorf("parsing chat message: %w", err)
	}

	// Strategy 1: Extract MEDIA: paths from message text
	images := inj.extractMediaPaths(&msg)
	mediaPathCount := len(images)

	// Strategy 2: Fall back to directory scanning (files within MaxAge window)
	var dirScanCount int
	if len(images) == 0 {
		images = inj.scanImages()
		dirScanCount = len(images)
	}

	if len(images) == 0 {
		slog.Info("media: no images found for final message",
			"runId", chat.RunID,
			"mediaPaths", mediaPathCount,
			"directoryScan", dirScanCount,
			"directory", inj.cfg.Directory,
			"runStart", runStart,
		)
		// No images found, return original payload
		return json.Marshal(outer)
	}

	source := "media_paths"
	if mediaPathCount == 0 {
		source = "directory_scan"
	}

	msg.Content = append(msg.Content, images...)
	slog.Info("media: injected media into chat message",
		"runId", chat.RunID,
		"mediaCount", len(images),
		"source", source,
	)

	// Rebuild the message chain: message → payload → outer
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshaling message: %w", err)
	}
	chat.Message = msgBytes

	payloadBytes, err := json.Marshal(chat)
	if err != nil {
		return nil, fmt.Errorf("marshaling payload: %w", err)
	}
	outer.Payload = payloadBytes

	return json.Marshal(outer)
}

// extractMediaPaths looks for "MEDIA: /path/to/file" lines in the message text
// content items and reads matching image files.
func (inj *Injector) extractMediaPaths(msg *chatMessage) []contentItem {
	extSet := make(map[string]bool, len(inj.cfg.Extensions))
	for _, ext := range inj.cfg.Extensions {
		extSet[strings.ToLower(ext)] = true
	}

	// Size budget: reserve 64KB for JSON envelope overhead.
	maxPayload := inj.cfg.MaxFileSize // per-file limit
	const envelopeOverhead = 65536
	var totalB64Size int64
	budgetMax := int64(0)
	if inj.cfg.MaxFileSize > 0 {
		// Use max_message_size as the total budget if available via MaxFileSize proxy.
		// Base64 expands by 4/3, so budget = (max_message_size - overhead) * 3/4.
		budgetMax = (maxPayload*4/3 + envelopeOverhead) * 10 // generous total budget
	}

	var items []contentItem
	var totalMarkers, skippedExt, skippedPath, skippedAccess, skippedSize, skippedRead, skippedBudget int
	for i, ci := range msg.Content {
		if ci.Type != "text" {
			continue
		}
		matches := mediaPathRe.FindAllStringSubmatch(ci.Text, -1)
		totalMarkers += len(matches)

		// Strip MEDIA: markers from text content after processing.
		if len(matches) > 0 {
			cleaned := mediaPathRe.ReplaceAllString(ci.Text, "")
			cleaned = strings.TrimRight(cleaned, "\n")
			msg.Content[i].Text = cleaned
		}

		for _, m := range matches {
			filePath := m[1]
			ext := strings.ToLower(filepath.Ext(filePath))
			if !extSet[ext] {
				slog.Debug("media: MEDIA path has non-matching extension", "path", filePath, "ext", ext)
				skippedExt++
				continue
			}

			// Path allowlist check.
			if !inj.isPathAllowed(filePath) {
				slog.Warn("media: MEDIA path outside allowed directories", "path", filePath, "allowed_dirs", inj.allowedDirs)
				skippedPath++
				continue
			}

			info, err := os.Stat(filePath)
			if err != nil {
				slog.Warn("media: MEDIA path not accessible", "path", filePath, "error", err)
				skippedAccess++
				continue
			}
			if info.Size() > inj.cfg.MaxFileSize {
				slog.Warn("media: MEDIA path file too large", "path", filePath, "size", info.Size(), "maxFileSize", inj.cfg.MaxFileSize)
				skippedSize++
				continue
			}

			// Size budget check: will this file's base64 fit in the message?
			b64Size := (info.Size()*4 + 2) / 3 // ceiling of 4/3
			if budgetMax > 0 && totalB64Size+b64Size+envelopeOverhead > budgetMax {
				slog.Warn("media: skipping file, total base64 size would exceed message budget",
					"path", filePath, "fileB64Size", b64Size, "currentTotal", totalB64Size)
				skippedBudget++
				continue
			}

			data, err := os.ReadFile(filePath)
			if err != nil {
				slog.Warn("media: failed to read MEDIA path", "path", filePath, "error", err)
				skippedRead++
				continue
			}

			mimeType := mimeFromExt(ext)
			encoded := base64.StdEncoding.EncodeToString(data)
			contentType := "image"
			if !strings.HasPrefix(mimeType, "image/") {
				contentType = "file"
			}
			totalB64Size += int64(len(encoded))
			items = append(items, contentItem{
				Type:     contentType,
				MimeType: mimeType,
				Content:  encoded,
				FileName: filepath.Base(filePath),
				FileSize: info.Size(),
			})
			slog.Debug("media: extracted media from MEDIA path",
				"path", filePath,
				"size", info.Size(),
				"mimeType", mimeType,
				"contentType", contentType,
			)
		}
	}

	slog.Debug("media: extractMediaPaths complete",
		"totalMarkers", totalMarkers,
		"extracted", len(items),
		"skippedExt", skippedExt,
		"skippedPath", skippedPath,
		"skippedAccess", skippedAccess,
		"skippedSize", skippedSize,
		"skippedRead", skippedRead,
		"skippedBudget", skippedBudget,
		"totalB64Size", totalB64Size,
	)
	return items
}

// scanImages looks for files in the media directory that were modified
// within the MaxAge window and match the configured extensions.
func (inj *Injector) scanImages() []contentItem {
	if inj.cfg.Directory == "" {
		slog.Debug("media: directory scan skipped, no directory configured")
		return nil
	}

	entries, err := os.ReadDir(inj.cfg.Directory)
	if err != nil {
		slog.Warn("media: failed to read media directory", "dir", inj.cfg.Directory, "error", err)
		return nil
	}

	extSet := make(map[string]bool, len(inj.cfg.Extensions))
	for _, ext := range inj.cfg.Extensions {
		extSet[strings.ToLower(ext)] = true
	}

	var items []contentItem
	var totalFiles, wrongExt, tooOld, tooLarge int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		totalFiles++

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !extSet[ext] {
			wrongExt++
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Only consider files within the MaxAge window
		if time.Since(info.ModTime()) > inj.cfg.MaxAge {
			tooOld++
			continue
		}

		// Skip files that exceed MaxFileSize
		if info.Size() > inj.cfg.MaxFileSize {
			slog.Warn("media: skipping oversized file", "file", entry.Name(), "size", info.Size(), "maxFileSize", inj.cfg.MaxFileSize)
			tooLarge++
			continue
		}

		fullPath := filepath.Join(inj.cfg.Directory, entry.Name())
		data, err := os.ReadFile(fullPath)
		if err != nil {
			slog.Warn("media: failed to read image file", "file", fullPath, "error", err)
			continue
		}

		mimeType := mimeFromExt(ext)
		encoded := base64.StdEncoding.EncodeToString(data)
		contentType := "image"
		if !strings.HasPrefix(mimeType, "image/") {
			contentType = "file"
		}

		items = append(items, contentItem{
			Type:     contentType,
			MimeType: mimeType,
			Content:  encoded,
			FileName: entry.Name(),
			FileSize: info.Size(),
		})

		slog.Debug("media: found media for injection",
			"file", entry.Name(),
			"size", info.Size(),
			"mimeType", mimeType,
			"contentType", contentType,
		)
	}

	slog.Debug("media: directory scan complete",
		"dir", inj.cfg.Directory,
		"totalFiles", totalFiles,
		"wrongExt", wrongExt,
		"tooOld", tooOld,
		"tooLarge", tooLarge,
		"matched", len(items),
	)

	return items
}

// mimeFromExt returns the MIME type for a file extension.
func mimeFromExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	case ".doc", ".docx":
		return "application/msword"
	case ".mp3":
		return "audio/mpeg"
	case ".mp4":
		return "video/mp4"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}
