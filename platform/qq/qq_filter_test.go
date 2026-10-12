package qq

import (
	"strconv"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestNew_MentionOnlyAndAllowGroups(t *testing.T) {
	p, err := New(map[string]any{
		"mention_only": true,
		"allow_groups": "111, 222",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pl := p.(*Platform)
	if !pl.mentionOnly {
		t.Error("mentionOnly = false, want true")
	}
	if !pl.isGroupAllowed(111) || !pl.isGroupAllowed(222) || pl.isGroupAllowed(333) {
		t.Errorf("allowGroups = %v, want {111,222}", pl.allowGroups)
	}
}

func TestNew_InvalidAllowGroups(t *testing.T) {
	if _, err := New(map[string]any{"allow_groups": "abc"}); err == nil {
		t.Error("expected error for non-numeric group ID")
	}
}

func TestParseAllowGroups(t *testing.T) {
	tests := []struct {
		name    string
		in      any
		want    []int64 // nil = all groups allowed
		wantErr bool
	}{
		{name: "nil", in: nil},
		{name: "empty", in: ""},
		{name: "wildcard", in: "*"},
		{name: "wildcard in list", in: "1,*"},
		{name: "string list", in: "1, 2 ,,3", want: []int64{1, 2, 3}},
		{name: "toml int array", in: []any{int64(10), int64(20)}, want: []int64{10, 20}},
		{name: "toml string array", in: []any{"10", " 20 "}, want: []int64{10, 20}},
		{name: "invalid", in: "x", wantErr: true},
		{name: "bad type", in: true, wantErr: true},
		{name: "bad element", in: []any{true}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAllowGroups(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want == nil {
				if got != nil {
					t.Errorf("got %v, want nil (all groups)", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for _, g := range tt.want {
				if !got[g] {
					t.Errorf("group %d missing from %v", g, got)
				}
			}
		})
	}
}

func TestIsMentioned(t *testing.T) {
	const self = int64(10001)
	seg := func(qq any) map[string]any {
		return map[string]any{"type": "at", "data": map[string]any{"qq": qq}}
	}
	text := map[string]any{"type": "text", "data": map[string]any{"text": "hi"}}

	tests := []struct {
		name    string
		payload map[string]any
		want    bool
	}{
		{"at segment string", map[string]any{"message": []any{seg("10001"), text}}, true},
		{"at segment number", map[string]any{"message": []any{text, seg(float64(10001))}}, true},
		{"at other user", map[string]any{"message": []any{seg("20002"), text}}, false},
		{"at all", map[string]any{"message": []any{seg("all"), text}}, false},
		{"no at", map[string]any{"message": []any{text}}, false},
		{"cq string", map[string]any{"message": "[CQ:at,qq=10001] hi"}, true},
		{"cq string with name", map[string]any{"message": "[CQ:at,qq=10001,name=bot] hi"}, true},
		{"cq string other", map[string]any{"message": "[CQ:at,qq=100011] hi"}, false},
		{"raw_message fallback", map[string]any{"raw_message": "hey [CQ:at,qq=10001]"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMentioned(tt.payload, self); got != tt.want {
				t.Errorf("isMentioned = %v, want %v", got, tt.want)
			}
		})
	}
	if isMentioned(map[string]any{"message": []any{seg("0")}}, 0) {
		t.Error("isMentioned with unknown selfID should be false")
	}
}

var qqTestMsgID int64 = 1000

func groupPayload(groupID int64, segs ...any) map[string]any {
	qqTestMsgID++
	return map[string]any{
		"post_type":    "message",
		"message_type": "group",
		"self_id":      float64(10001),
		"user_id":      float64(42),
		"group_id":     float64(groupID),
		"message_id":   float64(qqTestMsgID),
		"time":         float64(time.Now().Unix()),
		"message":      segs,
	}
}

func newFilterTestPlatform(t *testing.T, opts map[string]any) (*Platform, *[]*core.Message) {
	t.Helper()
	p, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pl := p.(*Platform)
	var got []*core.Message
	pl.handler = func(_ core.Platform, m *core.Message) { got = append(got, m) }
	// Pre-populate the group-name cache so handleMessage does not hit the API.
	for _, g := range []int64{111, 222} {
		pl.groupNameCache.Store(strconv.FormatInt(g, 10), "g")
	}
	return pl, &got
}

func TestHandleMessage_MentionOnly(t *testing.T) {
	pl, got := newFilterTestPlatform(t, map[string]any{"mention_only": true})
	pl.selfID = 10001

	text := map[string]any{"type": "text", "data": map[string]any{"text": " hello"}}
	at := map[string]any{"type": "at", "data": map[string]any{"qq": "10001"}}

	pl.handleMessage(groupPayload(111, text))
	if len(*got) != 0 {
		t.Fatalf("message without @bot should be ignored, got %d", len(*got))
	}

	pl.handleMessage(groupPayload(111, at, text))
	if len(*got) != 1 || (*got)[0].Content != "hello" {
		t.Fatalf("message with @bot should be delivered, got %+v", *got)
	}

	// Private messages are not affected by mention_only.
	qqTestMsgID++
	pl.handleMessage(map[string]any{
		"post_type":    "message",
		"message_type": "private",
		"user_id":      float64(42),
		"message_id":   float64(qqTestMsgID),
		"time":         float64(time.Now().Unix()),
		"message":      []any{text},
	})
	if len(*got) != 2 {
		t.Fatalf("private message should be delivered, got %d", len(*got))
	}
}

func TestHandleMessage_MentionOnlyFallsBackToEventSelfID(t *testing.T) {
	pl, got := newFilterTestPlatform(t, map[string]any{"mention_only": true})
	// selfID unknown (get_login_info failed); payload self_id is used instead.
	at := map[string]any{"type": "at", "data": map[string]any{"qq": "10001"}}
	text := map[string]any{"type": "text", "data": map[string]any{"text": "hi"}}
	pl.handleMessage(groupPayload(111, at, text))
	if len(*got) != 1 {
		t.Fatalf("expected delivery via event self_id, got %d", len(*got))
	}
}

func TestHandleMessage_AllowGroups(t *testing.T) {
	pl, got := newFilterTestPlatform(t, map[string]any{"allow_groups": "111"})
	pl.selfID = 10001
	text := map[string]any{"type": "text", "data": map[string]any{"text": "hi"}}

	pl.handleMessage(groupPayload(222, text))
	if len(*got) != 0 {
		t.Fatalf("message from non-allowed group should be ignored, got %d", len(*got))
	}
	pl.handleMessage(groupPayload(111, text))
	if len(*got) != 1 {
		t.Fatalf("message from allowed group should be delivered, got %d", len(*got))
	}
}
