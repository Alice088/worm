package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	raw, err := marshalRaw(CallStatePayload{
		CallID: "abc123",
		State:  "connected",
		Role:   "caller",
		Self:   Party{ID: "u1", Name: "alice"},
		Peer:   Party{ID: "u2", Name: "bob"},
		Invite: "token",
	})
	if err != nil {
		t.Fatal(err)
	}

	env := Envelope{Type: TypeCallState, RequestID: "req-1", CallID: "abc123", Payload: raw}
	data, err := env.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(string(data), `"version":1`) {
		t.Fatalf("missing version: %s", data)
	}

	got, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Type != TypeCallState || got.CallID != "abc123" || got.RequestID != "req-1" {
		t.Fatalf("unexpected envelope: %+v", got)
	}

	var payload CallStatePayload
	if err := got.DecodePayload(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.State != "connected" || payload.Peer.Name != "bob" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	raw := []byte(`{"version":1,"type":"ping","unexpected":true}`)
	if _, err := Decode(raw); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
}

func TestDecodeRejectsBadVersion(t *testing.T) {
	raw := []byte(`{"version":9,"type":"ping"}`)
	_, err := Decode(raw)
	if err == nil {
		t.Fatal("expected version error")
	}
	if AsError(err).Code != CodeInvalidMessage {
		t.Fatalf("expected invalid_message, got %v", AsError(err).Code)
	}
}

func TestDecodeRejectsUnknownType(t *testing.T) {
	raw := []byte(`{"version":1,"type":"call.hack"}`)
	if _, err := Decode(raw); err == nil {
		t.Fatal("expected type error")
	}
}

func TestDecodeRequiresCallIDForCallScopedTypes(t *testing.T) {
	raw := []byte(`{"version":1,"type":"webrtc.offer","payload":{"sdp":"v=0"}}`)
	_, err := Decode(raw)
	if err == nil {
		t.Fatal("expected call_id error")
	}
}

func TestDecodeRejectsOversizedMessage(t *testing.T) {
	huge := `{"version":1,"type":"ping","payload":{"nonce":"` + strings.Repeat("a", MaxMessageBytes) + `"}}`
	if _, err := Decode([]byte(huge)); err == nil {
		t.Fatal("expected size error")
	}
}

func TestPayloadValidation(t *testing.T) {
	if err := (SDPPayload{SDP: ""}).Validate(); err == nil {
		t.Fatal("empty sdp must be rejected")
	}
	if err := (SDPPayload{SDP: strings.Repeat("v", MaxSDPBytes+1)}).Validate(); err == nil {
		t.Fatal("oversized sdp must be rejected")
	}
	if err := (SDPPayload{SDP: "v=0"}).Validate(); err != nil {
		t.Fatalf("valid sdp rejected: %v", err)
	}
	if err := (ICECandidatePayload{Candidate: strings.Repeat("c", MaxCandidateBytes+1)}).Validate(); err == nil {
		t.Fatal("oversized candidate must be rejected")
	}
	if err := (ICECandidatePayload{Candidate: "candidate:1"}).Validate(); err != nil {
		t.Fatalf("valid candidate rejected: %v", err)
	}
}

func TestValidateName(t *testing.T) {
	for _, name := range []string{"alice", "Имя", "a b"} {
		if err := ValidateName(name); err != nil {
			t.Fatalf("%q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"", "   ", strings.Repeat("a", 33), "bad\nname"} {
		if err := ValidateName(name); err == nil {
			t.Fatalf("%q accepted", name)
		}
	}
}

func TestValidateReason(t *testing.T) {
	if _, err := ValidateReason(strings.Repeat("r", MaxReasonLen+1)); err == nil {
		t.Fatal("oversized reason must be rejected")
	}
	got, err := ValidateReason("  hangup  ")
	if err != nil || got != "hangup" {
		t.Fatalf("unexpected reason %q err %v", got, err)
	}
}

func TestErrorPayloadPreservesRequestID(t *testing.T) {
	payload := ErrorPayload{Code: CodeInvalidState, Message: "nope", RequestID: "req-9"}
	raw, err := marshalRaw(payload)
	if err != nil {
		t.Fatal(err)
	}
	env := Envelope{Version: Version, Type: TypeCallError, Payload: raw}
	out, err := env.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	var back ErrorPayload
	if err := decoded.DecodePayload(&back); err != nil {
		t.Fatal(err)
	}
	if back.RequestID != "req-9" || back.Code != CodeInvalidState {
		t.Fatalf("unexpected payload: %+v", back)
	}
}

func TestDecodeRejectsInvalidRequestIDCharacters(t *testing.T) {
	raw := []byte(`{"version":1,"type":"ping","request_id":"has space"}`)
	if _, err := Decode(raw); err == nil {
		t.Fatal("expected request_id validation error")
	}
}

func TestCallStateDeadlineEncoding(t *testing.T) {
	deadline := time.Now().Add(time.Minute).UTC().Truncate(time.Second)
	raw, err := marshalRaw(CallStatePayload{
		CallID:   "c1",
		State:    "accepted",
		Deadline: &deadline,
	})
	if err != nil {
		t.Fatal(err)
	}
	var back CallStatePayload
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Deadline == nil || !back.Deadline.Equal(deadline) {
		t.Fatalf("deadline mismatch: %v", back.Deadline)
	}
}

func marshalRaw(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}
