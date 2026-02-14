package media

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cortexuvula/clawreachbridge/internal/config"
)

func testConfig(dir string) config.MediaConfig {
	return config.MediaConfig{
		Enabled:     true,
		Directory:   dir,
		MaxFileSize: 5 * 1024 * 1024,
		MaxAge:      60 * time.Second,
		Extensions:  []string{".png", ".jpg", ".jpeg", ".webp", ".gif"},
	}
}

func makeChatMessage(state, runID, text string) []byte {
	msg := chatMessage{
		Role: "assistant",
		Content: []contentItem{
			{Type: "text", Text: text},
		},
		Timestamp: time.Now().UnixMilli(),
	}
	msgBytes, _ := json.Marshal(msg)

	payload := chatPayload{
		RunID:   runID,
		State:   state,
		Message: msgBytes,
	}
	payloadBytes, _ := json.Marshal(payload)

	outer := outerMessage{
		Type:    "event",
		Event:   "chat",
		Payload: payloadBytes,
	}
	result, _ := json.Marshal(outer)
	return result
}

func TestProcessMessage_Delta_TracksRun(t *testing.T) {
	inj := NewInjector(testConfig(""))

	delta := makeChatMessage("delta", "run-123", "")
	result := inj.ProcessMessage(delta)

	// Delta should be returned unchanged
	if string(result) != string(delta) {
		t.Error("delta message should be returned unchanged")
	}

	// Run should be tracked
	inj.mu.Lock()
	_, tracked := inj.runStarts["run-123"]
	inj.mu.Unlock()

	if !tracked {
		t.Error("run-123 should be tracked after delta")
	}
}

func TestProcessMessage_Final_InjectsImages(t *testing.T) {
	// Create temp dir with an image
	dir := t.TempDir()
	imgData := []byte("fake-png-data")
	imgPath := filepath.Join(dir, "test-image.png")
	if err := os.WriteFile(imgPath, imgData, 0644); err != nil {
		t.Fatal(err)
	}

	inj := NewInjector(testConfig(dir))

	// Track a delta first
	delta := makeChatMessage("delta", "run-456", "")
	inj.ProcessMessage(delta)

	// Small delay so the image mtime is after run start
	time.Sleep(10 * time.Millisecond)

	// Touch the image file to ensure its mtime is after run start
	now := time.Now()
	os.Chtimes(imgPath, now, now)

	// Now send final
	final := makeChatMessage("final", "run-456", "Here's your image!")
	result := inj.ProcessMessage(final)

	// Parse the result
	var outer outerMessage
	if err := json.Unmarshal(result, &outer); err != nil {
		t.Fatal(err)
	}

	var chat chatPayload
	if err := json.Unmarshal(outer.Payload, &chat); err != nil {
		t.Fatal(err)
	}

	var msg chatMessage
	if err := json.Unmarshal(chat.Message, &msg); err != nil {
		t.Fatal(err)
	}

	if len(msg.Content) != 2 {
		t.Fatalf("expected 2 content items, got %d", len(msg.Content))
	}

	if msg.Content[0].Type != "text" {
		t.Errorf("first content item should be text, got %s", msg.Content[0].Type)
	}
	if msg.Content[0].Text != "Here's your image!" {
		t.Errorf("text content mismatch: %s", msg.Content[0].Text)
	}

	if msg.Content[1].Type != "image" {
		t.Errorf("second content item should be image, got %s", msg.Content[1].Type)
	}
	if msg.Content[1].MimeType != "image/png" {
		t.Errorf("expected image/png mime type, got %s", msg.Content[1].MimeType)
	}

	decoded, err := base64.StdEncoding.DecodeString(msg.Content[1].Content)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(imgData) {
		t.Error("decoded image data doesn't match")
	}

	// Run should be cleaned up
	inj.mu.Lock()
	_, stillTracked := inj.runStarts["run-456"]
	inj.mu.Unlock()

	if stillTracked {
		t.Error("run-456 should be cleaned up after final")
	}
}

func TestProcessMessage_NonChat_PassThrough(t *testing.T) {
	inj := NewInjector(testConfig(""))

	// A non-chat event message
	msg := `{"type":"event","event":"status","payload":{"state":"connected"}}`
	result := inj.ProcessMessage([]byte(msg))

	if string(result) != msg {
		t.Error("non-chat message should be returned unchanged")
	}
}

func TestProcessMessage_NonEvent_PassThrough(t *testing.T) {
	inj := NewInjector(testConfig(""))

	msg := `{"type":"request","method":"ping"}`
	result := inj.ProcessMessage([]byte(msg))

	if string(result) != msg {
		t.Error("non-event message should be returned unchanged")
	}
}

func TestProcessMessage_Final_NoImages(t *testing.T) {
	// Empty temp dir — no images to inject
	dir := t.TempDir()
	inj := NewInjector(testConfig(dir))

	delta := makeChatMessage("delta", "run-789", "")
	inj.ProcessMessage(delta)

	final := makeChatMessage("final", "run-789", "No images here")
	result := inj.ProcessMessage(final)

	var outer outerMessage
	if err := json.Unmarshal(result, &outer); err != nil {
		t.Fatal(err)
	}

	var chat chatPayload
	if err := json.Unmarshal(outer.Payload, &chat); err != nil {
		t.Fatal(err)
	}

	var msg chatMessage
	if err := json.Unmarshal(chat.Message, &msg); err != nil {
		t.Fatal(err)
	}

	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content item (text only), got %d", len(msg.Content))
	}
}

func TestProcessMessage_SkipsOversizedFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a "large" file that exceeds MaxFileSize
	cfg := testConfig(dir)
	cfg.MaxFileSize = 10 // 10 bytes max

	bigData := make([]byte, 100)
	if err := os.WriteFile(filepath.Join(dir, "big.png"), bigData, 0644); err != nil {
		t.Fatal(err)
	}

	inj := NewInjector(cfg)
	delta := makeChatMessage("delta", "run-big", "")
	inj.ProcessMessage(delta)

	time.Sleep(10 * time.Millisecond)
	now := time.Now()
	os.Chtimes(filepath.Join(dir, "big.png"), now, now)

	final := makeChatMessage("final", "run-big", "text")
	result := inj.ProcessMessage(final)

	var outer outerMessage
	json.Unmarshal(result, &outer)
	var chat chatPayload
	json.Unmarshal(outer.Payload, &chat)
	var msg chatMessage
	json.Unmarshal(chat.Message, &msg)

	if len(msg.Content) != 1 {
		t.Errorf("expected 1 content item (oversized file should be skipped), got %d", len(msg.Content))
	}
}

func TestProcessMessage_SkipsNonImageExtensions(t *testing.T) {
	dir := t.TempDir()

	// Create files with non-image extensions
	os.WriteFile(filepath.Join(dir, "audio.mp3"), []byte("audio"), 0644)
	os.WriteFile(filepath.Join(dir, "doc.pdf"), []byte("pdf"), 0644)
	os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# notes"), 0644)

	inj := NewInjector(testConfig(dir))
	delta := makeChatMessage("delta", "run-ext", "")
	inj.ProcessMessage(delta)

	time.Sleep(10 * time.Millisecond)
	now := time.Now()
	for _, name := range []string{"audio.mp3", "doc.pdf", "notes.md"} {
		os.Chtimes(filepath.Join(dir, name), now, now)
	}

	final := makeChatMessage("final", "run-ext", "text")
	result := inj.ProcessMessage(final)

	var outer outerMessage
	json.Unmarshal(result, &outer)
	var chat chatPayload
	json.Unmarshal(outer.Payload, &chat)
	var msg chatMessage
	json.Unmarshal(chat.Message, &msg)

	if len(msg.Content) != 1 {
		t.Errorf("expected 1 content item (non-image files should be skipped), got %d", len(msg.Content))
	}
}

func TestProcessMessage_MediaPath_InjectsImage(t *testing.T) {
	// Create a temp image file at a known path
	dir := t.TempDir()
	imgData := []byte("media-path-image-data")
	imgPath := filepath.Join(dir, "generated.png")
	if err := os.WriteFile(imgPath, imgData, 0644); err != nil {
		t.Fatal(err)
	}

	// Use empty directory for scan (so dir scan finds nothing),
	// but allow the file dir for MEDIA: path resolution.
	emptyDir := t.TempDir()
	cfg := testConfig(emptyDir)
	cfg.AllowedDirs = []string{dir, emptyDir}
	inj := NewInjector(cfg)

	// Track a delta
	delta := makeChatMessage("delta", "run-media", "")
	inj.ProcessMessage(delta)

	// Send final with MEDIA: path in text
	text := "Here's your picture!\n\nMEDIA: " + imgPath
	final := makeChatMessage("final", "run-media", text)
	result := inj.ProcessMessage(final)

	var outer outerMessage
	json.Unmarshal(result, &outer)
	var chat chatPayload
	json.Unmarshal(outer.Payload, &chat)
	var msg chatMessage
	json.Unmarshal(chat.Message, &msg)

	if len(msg.Content) != 2 {
		t.Fatalf("expected 2 content items (text + image from MEDIA path), got %d", len(msg.Content))
	}
	if msg.Content[1].Type != "image" {
		t.Errorf("second content item should be image, got %s", msg.Content[1].Type)
	}
	if msg.Content[1].MimeType != "image/png" {
		t.Errorf("expected image/png, got %s", msg.Content[1].MimeType)
	}

	// MEDIA: marker should be stripped from text
	if msg.Content[0].Text != "Here's your picture!" {
		t.Errorf("MEDIA marker should be stripped from text, got: %q", msg.Content[0].Text)
	}

	decoded, err := base64.StdEncoding.DecodeString(msg.Content[1].Content)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(imgData) {
		t.Error("decoded image data doesn't match")
	}
}

func TestProcessMessage_MediaPath_SkipsNonImage(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	os.WriteFile(pdfPath, []byte("pdf-data"), 0644)

	emptyDir := t.TempDir()
	inj := NewInjector(testConfig(emptyDir))

	delta := makeChatMessage("delta", "run-pdf", "")
	inj.ProcessMessage(delta)

	text := "Generated a PDF\n\nMEDIA: " + pdfPath
	final := makeChatMessage("final", "run-pdf", text)
	result := inj.ProcessMessage(final)

	var outer outerMessage
	json.Unmarshal(result, &outer)
	var chat chatPayload
	json.Unmarshal(outer.Payload, &chat)
	var msg chatMessage
	json.Unmarshal(chat.Message, &msg)

	if len(msg.Content) != 1 {
		t.Errorf("expected 1 content item (PDF should be skipped), got %d", len(msg.Content))
	}
}

func TestProcessMessage_MediaPath_MultipleImages(t *testing.T) {
	dir := t.TempDir()
	img1 := filepath.Join(dir, "photo1.jpg")
	img2 := filepath.Join(dir, "photo2.png")
	os.WriteFile(img1, []byte("jpg-data"), 0644)
	os.WriteFile(img2, []byte("png-data"), 0644)

	emptyDir := t.TempDir()
	cfg := testConfig(emptyDir)
	cfg.AllowedDirs = []string{dir, emptyDir}
	inj := NewInjector(cfg)

	delta := makeChatMessage("delta", "run-multi", "")
	inj.ProcessMessage(delta)

	text := "Here are two images\n\nMEDIA: " + img1 + "\nMEDIA: " + img2
	final := makeChatMessage("final", "run-multi", text)
	result := inj.ProcessMessage(final)

	var outer outerMessage
	json.Unmarshal(result, &outer)
	var chat chatPayload
	json.Unmarshal(outer.Payload, &chat)
	var msg chatMessage
	json.Unmarshal(chat.Message, &msg)

	if len(msg.Content) != 3 {
		t.Fatalf("expected 3 content items (text + 2 images), got %d", len(msg.Content))
	}
	if msg.Content[1].MimeType != "image/jpeg" {
		t.Errorf("first image should be jpeg, got %s", msg.Content[1].MimeType)
	}
	if msg.Content[2].MimeType != "image/png" {
		t.Errorf("second image should be png, got %s", msg.Content[2].MimeType)
	}
}

func TestProcessMessage_InvalidJSON_PassThrough(t *testing.T) {
	inj := NewInjector(testConfig(""))

	garbage := []byte("not valid json{{{")
	result := inj.ProcessMessage(garbage)

	if string(result) != string(garbage) {
		t.Error("invalid JSON should be returned unchanged")
	}
}
