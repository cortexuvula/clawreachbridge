package chatsync

import (
	"testing"
)

func TestMessageStoreAppendAndRetrieve(t *testing.T) {
	store := NewMessageStore(100)

	store.Append("session-1", StoredMessage{
		ID: "msg-1", Role: "user",
		Content:   []ContentItem{{Type: "text", Text: "hello"}},
		Timestamp: 1000,
	})
	store.Append("session-1", StoredMessage{
		ID: "msg-2", Role: "assistant",
		Content:   []ContentItem{{Type: "text", Text: "hi there"}},
		Timestamp: 2000,
	})

	msgs := store.GetHistory("session-1", 0)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].ID != "msg-1" {
		t.Errorf("first message ID = %q, want %q", msgs[0].ID, "msg-1")
	}
	if msgs[1].ID != "msg-2" {
		t.Errorf("second message ID = %q, want %q", msgs[1].ID, "msg-2")
	}
}

func TestMessageStoreRingBuffer(t *testing.T) {
	store := NewMessageStore(3)

	for i := 0; i < 5; i++ {
		store.Append("s", StoredMessage{
			ID:        "msg-" + string(rune('a'+i)),
			Role:      "user",
			Content:   []ContentItem{{Type: "text", Text: "test"}},
			Timestamp: int64(i),
		})
	}

	msgs := store.GetHistory("s", 0)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (maxSize), got %d", len(msgs))
	}
	// Should have the last 3: c, d, e
	if msgs[0].ID != "msg-c" {
		t.Errorf("oldest retained = %q, want %q", msgs[0].ID, "msg-c")
	}
	if msgs[2].ID != "msg-e" {
		t.Errorf("newest = %q, want %q", msgs[2].ID, "msg-e")
	}
}

func TestMessageStoreLimit(t *testing.T) {
	store := NewMessageStore(100)

	for i := 0; i < 10; i++ {
		store.Append("s", StoredMessage{
			ID:        "msg",
			Role:      "user",
			Content:   []ContentItem{{Type: "text", Text: "test"}},
			Timestamp: int64(i),
		})
	}

	msgs := store.GetHistory("s", 3)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (limit), got %d", len(msgs))
	}
	// Should be the last 3 (timestamps 7, 8, 9)
	if msgs[0].Timestamp != 7 {
		t.Errorf("first limited message timestamp = %d, want 7", msgs[0].Timestamp)
	}
}

func TestMessageStoreEmptySession(t *testing.T) {
	store := NewMessageStore(100)

	msgs := store.GetHistory("nonexistent", 0)
	if msgs != nil {
		t.Errorf("expected nil for nonexistent session, got %v", msgs)
	}
}

func TestMessageStoreIsolatesSessions(t *testing.T) {
	store := NewMessageStore(100)

	store.Append("session-a", StoredMessage{ID: "a1", Role: "user", Content: []ContentItem{{Type: "text", Text: "a"}}, Timestamp: 1})
	store.Append("session-b", StoredMessage{ID: "b1", Role: "user", Content: []ContentItem{{Type: "text", Text: "b"}}, Timestamp: 2})

	msgsA := store.GetHistory("session-a", 0)
	msgsB := store.GetHistory("session-b", 0)

	if len(msgsA) != 1 || msgsA[0].ID != "a1" {
		t.Errorf("session-a: got %v", msgsA)
	}
	if len(msgsB) != 1 || msgsB[0].ID != "b1" {
		t.Errorf("session-b: got %v", msgsB)
	}
}

func TestMessageStoreCount(t *testing.T) {
	store := NewMessageStore(100)

	if store.Count("s") != 0 {
		t.Errorf("empty session count = %d, want 0", store.Count("s"))
	}

	store.Append("s", StoredMessage{ID: "1", Role: "user", Content: []ContentItem{{Type: "text", Text: "a"}}, Timestamp: 1})
	store.Append("s", StoredMessage{ID: "2", Role: "user", Content: []ContentItem{{Type: "text", Text: "b"}}, Timestamp: 2})

	if store.Count("s") != 2 {
		t.Errorf("count = %d, want 2", store.Count("s"))
	}
}

func TestMessageStoreReturnsCopy(t *testing.T) {
	store := NewMessageStore(100)
	store.Append("s", StoredMessage{ID: "1", Role: "user", Content: []ContentItem{{Type: "text", Text: "original"}}, Timestamp: 1})

	msgs := store.GetHistory("s", 0)
	msgs[0].ID = "modified"

	msgs2 := store.GetHistory("s", 0)
	if msgs2[0].ID != "1" {
		t.Errorf("store was modified through returned slice: got ID %q", msgs2[0].ID)
	}
}
