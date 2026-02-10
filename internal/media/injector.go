package media

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cortexuvula/clawreachbridge/internal/config"
)

// Injector tracks chat runs and injects images from the gateway's media
// directory into final chat messages before they reach the client.
type Injector struct {
	cfg       config.MediaConfig
	mu        sync.Mutex
	runStarts map[string]time.Time // runId → first delta timestamp
}

// NewInjector creates a media injector with the given config.
func NewInjector(cfg config.MediaConfig) *Injector {
	return &Injector{
		cfg:       cfg,
		runStarts: make(map[string]time.Time),
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

// contentItem is a single content element (text or image).
type contentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Content  string `json:"content,omitempty"`
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
		return payload
	case "final":
		enriched, err := inj.enrichFinal(&outer, &chat)
		if err != nil {
			slog.Warn("media: failed to enrich final message", "error", err)
			return payload
		}
		return enriched
	default:
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

// enrichFinal scans for new images and injects them into the final chat message.
func (inj *Injector) enrichFinal(outer *outerMessage, chat *chatPayload) ([]byte, error) {
	// Look up when this run started
	inj.mu.Lock()
	runStart, tracked := inj.runStarts[chat.RunID]
	delete(inj.runStarts, chat.RunID)
	inj.mu.Unlock()

	if !tracked {
		// No delta was seen for this run — use MaxAge as a fallback window
		runStart = time.Now().Add(-inj.cfg.MaxAge)
		slog.Debug("media: no tracked start for run, using maxAge fallback", "runId", chat.RunID)
	}

	// Scan for new images
	images := inj.scanImages(runStart)
	if len(images) == 0 {
		// No images found, return original payload
		return json.Marshal(outer)
	}

	// Parse the message to append image content items
	var msg chatMessage
	if err := json.Unmarshal(chat.Message, &msg); err != nil {
		return nil, fmt.Errorf("parsing chat message: %w", err)
	}

	msg.Content = append(msg.Content, images...)
	slog.Info("media: injected images into chat message",
		"runId", chat.RunID,
		"imageCount", len(images),
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

// scanImages looks for image files in the media directory that were created
// after runStart and match the configured extensions.
func (inj *Injector) scanImages(runStart time.Time) []contentItem {
	if inj.cfg.Directory == "" {
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
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !extSet[ext] {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Only consider files modified after the run started
		if info.ModTime().Before(runStart) {
			continue
		}

		// Skip files that are too old (beyond MaxAge from now)
		if time.Since(info.ModTime()) > inj.cfg.MaxAge {
			continue
		}

		// Skip files that exceed MaxFileSize
		if info.Size() > inj.cfg.MaxFileSize {
			slog.Debug("media: skipping oversized file", "file", entry.Name(), "size", info.Size())
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

		items = append(items, contentItem{
			Type:     "image",
			MimeType: mimeType,
			Content:  encoded,
		})

		slog.Debug("media: found image for injection",
			"file", entry.Name(),
			"size", info.Size(),
			"mimeType", mimeType,
		)
	}

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
	default:
		return "application/octet-stream"
	}
}
