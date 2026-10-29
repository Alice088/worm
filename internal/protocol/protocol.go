package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const Version = 1

const (
	MaxMessageBytes   = 512 << 10
	MaxSDPBytes       = 256 << 10
	MaxCandidateBytes = 8 << 10
	MaxRequestIDLen   = 64
	MaxCallIDLen      = 64
	MaxReasonLen      = 256
	MaxNameLen        = 32
)

type Type string

const (
	TypeCallCreate         Type = "call.create"
	TypeCallIncoming       Type = "call.incoming"
	TypeCallAccept         Type = "call.accept"
	TypeCallReject         Type = "call.reject"
	TypeCallCancel         Type = "call.cancel"
	TypeCallEnd            Type = "call.end"
	TypeCallState          Type = "call.state"
	TypeCallError          Type = "call.error"
	TypeWebRTCOffer        Type = "webrtc.offer"
	TypeWebRTCAnswer       Type = "webrtc.answer"
	TypeWebRTCICECandidate Type = "webrtc.ice_candidate"
	TypeWebRTCICEComplete  Type = "webrtc.ice_candidate_complete"
	TypeWebRTCRenegotiate  Type = "webrtc.renegotiate"
	TypeConnectionRecover  Type = "connection.recover"
	TypeConnectionStatus   Type = "connection.status"
	TypePing               Type = "ping"
	TypePong               Type = "pong"
)

var knownTypes = map[Type]bool{
	TypeCallCreate:         true,
	TypeCallIncoming:       true,
	TypeCallAccept:         true,
	TypeCallReject:         true,
	TypeCallCancel:         true,
	TypeCallEnd:            true,
	TypeCallState:          true,
	TypeCallError:          true,
	TypeWebRTCOffer:        true,
	TypeWebRTCAnswer:       true,
	TypeWebRTCICECandidate: true,
	TypeWebRTCICEComplete:  true,
	TypeWebRTCRenegotiate:  true,
	TypeConnectionRecover:  true,
	TypeConnectionStatus:   true,
	TypePing:               true,
	TypePong:               true,
}

var callScopedTypes = map[Type]bool{
	TypeCallAccept:         true,
	TypeCallReject:         true,
	TypeCallCancel:         true,
	TypeCallEnd:            true,
	TypeCallState:          true,
	TypeWebRTCOffer:        true,
	TypeWebRTCAnswer:       true,
	TypeWebRTCICECandidate: true,
	TypeWebRTCICEComplete:  true,
	TypeWebRTCRenegotiate:  true,
	TypeConnectionRecover:  true,
	TypeConnectionStatus:   true,
}

type ErrorCode string

const (
	CodeInvalidMessage  ErrorCode = "invalid_message"
	CodeInvalidState    ErrorCode = "invalid_state"
	CodeForbidden       ErrorCode = "forbidden"
	CodeNotFound        ErrorCode = "not_found"
	CodeExpired         ErrorCode = "call_expired"
	CodeUnavailable     ErrorCode = "participant_unavailable"
	CodeRateLimited     ErrorCode = "rate_limited"
	CodeTooLarge        ErrorCode = "message_too_large"
	CodeInternal        ErrorCode = "internal_error"
	CodeUnauthorized    ErrorCode = "unauthorized"
	CodeAlreadyInCall   ErrorCode = "already_in_call"
	CodeBadRequest      ErrorCode = "bad_request"
	CodeRequestConflict ErrorCode = "request_conflict"
)

var validErrorCodes = map[ErrorCode]bool{
	CodeInvalidMessage:  true,
	CodeInvalidState:    true,
	CodeForbidden:       true,
	CodeNotFound:        true,
	CodeExpired:         true,
	CodeUnavailable:     true,
	CodeRateLimited:     true,
	CodeTooLarge:        true,
	CodeInternal:        true,
	CodeUnauthorized:    true,
	CodeAlreadyInCall:   true,
	CodeBadRequest:      true,
	CodeRequestConflict: true,
}

type Error struct {
	Code     ErrorCode
	Message  string
	Internal error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error { return e.Internal }

func NewError(code ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func AsError(err error) *Error {
	var p *Error
	if errors.As(err, &p) {
		return p
	}
	return &Error{Code: CodeInternal, Message: err.Error(), Internal: err}
}

type ErrorPayload struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	RequestID string    `json:"request_id,omitempty"`
}

type Envelope struct {
	Version   int             `json:"version"`
	Type      Type            `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func (e Envelope) Encode() ([]byte, error) {
	e.Version = Version
	if err := e.validateHeader(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return nil, NewError(CodeInternal, "encode message: %v", err)
	}
	if len(data) > MaxMessageBytes {
		return nil, NewError(CodeTooLarge, "message exceeds %d bytes", MaxMessageBytes)
	}
	return data, nil
}

func Decode(data []byte) (Envelope, error) {
	var env Envelope
	if len(data) > MaxMessageBytes {
		return env, NewError(CodeTooLarge, "message exceeds %d bytes", MaxMessageBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		return env, NewError(CodeInvalidMessage, "malformed json: %v", err)
	}
	if err := env.validateHeader(); err != nil {
		return env, err
	}
	if callScopedTypes[env.Type] && env.CallID == "" {
		return env, NewError(CodeInvalidMessage, "call_id is required for %s", env.Type)
	}
	return env, nil
}

func (e Envelope) validateHeader() error {
	if e.Version != Version {
		return NewError(CodeInvalidMessage, "unsupported protocol version %d", e.Version)
	}
	if !knownTypes[e.Type] {
		return NewError(CodeInvalidMessage, "unknown message type %q", e.Type)
	}
	if len(e.RequestID) > MaxRequestIDLen {
		return NewError(CodeInvalidMessage, "request_id is too long")
	}
	if e.RequestID != "" && !isToken(e.RequestID) {
		return NewError(CodeInvalidMessage, "request_id has invalid characters")
	}
	if len(e.CallID) > MaxCallIDLen {
		return NewError(CodeInvalidMessage, "call_id is too long")
	}
	if e.CallID != "" && !isToken(e.CallID) {
		return NewError(CodeInvalidMessage, "call_id has invalid characters")
	}
	if len(e.Payload) > MaxMessageBytes {
		return NewError(CodeTooLarge, "payload exceeds %d bytes", MaxMessageBytes)
	}
	if len(e.Payload) > 0 && !json.Valid(e.Payload) {
		return NewError(CodeInvalidMessage, "payload is not valid json")
	}
	return nil
}

func (e Envelope) DecodePayload(dst any) error {
	if len(e.Payload) == 0 {
		return NewError(CodeInvalidMessage, "%s requires a payload", e.Type)
	}
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return NewError(CodeInvalidMessage, "invalid payload for %s: %v", e.Type, err)
	}
	return nil
}

type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type Party struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CallCreatePayload struct {
	CalleeID string `json:"callee_id,omitempty"`
}

type CallAcceptPayload struct {
	Invite string `json:"invite,omitempty"`
}

type CallRejectPayload struct {
	Invite string `json:"invite,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type CallEndPayload struct {
	Reason string `json:"reason,omitempty"`
}

type CallStatePayload struct {
	CallID    string     `json:"call_id"`
	State     string     `json:"state"`
	Role      string     `json:"role,omitempty"`
	Self      Party      `json:"self"`
	Peer      Party      `json:"peer"`
	Invite    string     `json:"invite,omitempty"`
	Deadline  *time.Time `json:"deadline,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	Connected *time.Time `json:"connected_at,omitempty"`
}

type SDPPayload struct {
	SDP string `json:"sdp"`
}

type ICECandidatePayload struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdp_mid,omitempty"`
	SDPMLineIndex    *uint16 `json:"sdp_mline_index,omitempty"`
	UsernameFragment *string `json:"username_fragment,omitempty"`
}

type ConnectionStatusPayload struct {
	PCState        string  `json:"pc_state"`
	ICEState       string  `json:"ice_state"`
	MediaState     string  `json:"media_state"`
	Relay          bool    `json:"relay"`
	RTTMillis      float64 `json:"rtt_ms,omitempty"`
	JitterMillis   float64 `json:"jitter_ms,omitempty"`
	PacketsLost    int64   `json:"packets_lost,omitempty"`
	PacketsRecv    int64   `json:"packets_received,omitempty"`
	RestartCount   int     `json:"restart_count,omitempty"`
	ReconnectTries int     `json:"reconnect_attempts,omitempty"`
}

type RecoverPayload struct {
	LastState string `json:"last_state,omitempty"`
}

type PingPayload struct {
	Nonce string `json:"nonce,omitempty"`
}

func (p SDPPayload) Validate() error {
	switch {
	case strings.TrimSpace(p.SDP) == "":
		return NewError(CodeInvalidMessage, "sdp is empty")
	case len(p.SDP) > MaxSDPBytes:
		return NewError(CodeTooLarge, "sdp exceeds %d bytes", MaxSDPBytes)
	case !utf8.ValidString(p.SDP):
		return NewError(CodeInvalidMessage, "sdp is not valid utf-8")
	}
	return nil
}

func (p ICECandidatePayload) Validate() error {
	switch {
	case len(p.Candidate) > MaxCandidateBytes:
		return NewError(CodeTooLarge, "candidate exceeds %d bytes", MaxCandidateBytes)
	case !utf8.ValidString(p.Candidate):
		return NewError(CodeInvalidMessage, "candidate is not valid utf-8")
	}
	if p.SDPMLineIndex != nil && *p.SDPMLineIndex > 64 {
		return NewError(CodeInvalidMessage, "sdp_mline_index is out of range")
	}
	if p.SDPMid != nil && len(*p.SDPMid) > 64 {
		return NewError(CodeInvalidMessage, "sdp_mid is too long")
	}
	if p.UsernameFragment != nil && len(*p.UsernameFragment) > 256 {
		return NewError(CodeInvalidMessage, "username_fragment is too long")
	}
	return nil
}

func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return NewError(CodeBadRequest, "name must not be empty")
	case len(name) > MaxNameLen:
		return NewError(CodeBadRequest, "name must be at most %d characters", MaxNameLen)
	case !utf8.ValidString(name):
		return NewError(CodeBadRequest, "name is not valid utf-8")
	case strings.ContainsAny(name, "\n\r\t"):
		return NewError(CodeBadRequest, "name must not contain control characters")
	}
	return nil
}

func ValidateReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) > MaxReasonLen {
		return "", NewError(CodeInvalidMessage, "reason exceeds %d characters", MaxReasonLen)
	}
	if !utf8.ValidString(reason) {
		return "", NewError(CodeInvalidMessage, "reason is not valid utf-8")
	}
	return reason, nil
}

func ValidateRequestID(id string) error {
	if id == "" {
		return nil
	}
	if len(id) > MaxRequestIDLen || !isToken(id) {
		return NewError(CodeInvalidMessage, "request_id has invalid characters")
	}
	return nil
}

func isToken(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == ':':
		default:
			return false
		}
	}
	return s != ""
}

func ValidErrorCode(code ErrorCode) bool { return validErrorCodes[code] }
