# P2P Voice Calling — Technical Specification

## 1. Goal

Implement reliable one-to-one audio calls using WebRTC with direct peer-to-peer media transport whenever possible and TURN relay as fallback.

The backend must handle signaling and call lifecycle, but must not process or relay audio.

## 2. Architecture

### Client
Responsible for:
- Microphone permissions and audio capture.
- WebRTC PeerConnection lifecycle.
- SDP offer/answer negotiation.
- ICE candidate gathering, exchange and connectivity checks.
- Audio playback and device selection.
- Connection state monitoring, recovery and resource cleanup.
- Reporting call and media quality statistics.

### Go Backend
Responsible for:
- Authentication and authorization.
- Call creation, invitation, acceptance, rejection and termination.
- Signaling message routing over authenticated WebSocket connections.
- Call state validation, timeouts and participant presence.
- Idempotency, duplicate message handling and stale message rejection.
- TURN credential issuance or integration with a credential provider.
- Structured logging, metrics and operational diagnostics.

### STUN/TURN
- STUN discovers server-reflexive network candidates.
- TURN relays encrypted WebRTC media when direct connectivity fails.
- TURN credentials must be temporary and authenticated.
- Do not assume that STUN alone guarantees connectivity.

## 3. Call Lifecycle

The server is authoritative for call control state. Each client independently tracks WebRTC connection state.

Server-side lifecycle:

`Ringing → Accepted → Connecting → Connected → Ended`

Alternative terminal states: `Rejected`, `Cancelled`, `Timeout`, `Failed`.

Requirements:
- Every call has a unique `call_id`.
- A call has exactly two authorized participants.
- Only the caller may cancel a pending outgoing call.
- Only the callee may accept or reject an incoming call.
- Either participant may terminate an active call.
- Terminal states are immutable.
- Duplicate requests must not create duplicate calls or repeat side effects.
- Every state transition must be validated atomically.
- Every call has configurable invitation and connection timeouts.
- Disconnecting a signaling socket must not immediately terminate an established media session. Apply a configurable reconnection grace period.
- The server must not mark a call connected solely because SDP negotiation completed. Use client-reported ICE/media state with server-side validation.

## 4. Signaling Protocol

Use WebSocket with versioned, typed JSON messages.

Every client message must include:
- `type`
- `request_id` — unique request identifier for idempotency where applicable.
- `call_id` — required for call-scoped messages.
- `payload` — message-specific data.

The server must validate schema, authorization, payload size, call membership and allowed message order.

Required message types:

### Call control
- `call.create`
- `call.incoming`
- `call.accept`
- `call.reject`
- `call.cancel`
- `call.end`
- `call.state`
- `call.error`

### WebRTC signaling
- `webrtc.offer`
- `webrtc.answer`
- `webrtc.ice_candidate`
- `webrtc.ice_candidate_complete`
- `webrtc.renegotiate`

### Connection lifecycle
- `connection.recover`
- `connection.status`
- `ping`
- `pong`

Signaling rules:
- Route SDP and ICE messages only to the other participant of the relevant call.
- Never trust a client-supplied participant identity without deriving or validating it against the authenticated session.
- Reject messages for terminal, unknown or unauthorized calls.
- Support ICE candidates arriving before the remote description is applied by buffering them on the client.
- Handle end-of-candidates explicitly.
- Preserve message ordering where required, or implement buffering and sequence validation.
- Use acknowledgments or defined retry behavior for critical control messages.
- Do not blindly retry non-idempotent operations.
- Define error codes for invalid state, forbidden action, expired call, unavailable participant and internal failure.

## 5. WebRTC Connection Procedure

1. Caller requests a call.
2. Backend authenticates the caller, creates the call and notifies the callee.
3. Callee accepts; backend transitions the call to `Accepted`.
4. Caller creates a PeerConnection with configured ICE servers and attaches the audio track.
5. Caller creates and sets the local SDP offer, then sends it through signaling.
6. Callee applies the remote offer, creates and sets the local SDP answer, and returns it.
7. Both clients exchange trickled ICE candidates through signaling.
8. ICE checks candidate pairs and selects a working route.
9. Clients monitor ICE, PeerConnection and media track states.
10. Call is considered connected only after the connection is usable, according to a defined client-side readiness policy.
11. On termination, both clients close the PeerConnection, stop media tracks and release audio devices.

Use established WebRTC APIs and libraries. Do not implement custom audio encryption, RTP, congestion control or ICE unless explicitly required by a separate project decision.

## 6. Recovery and Failure Handling

Implement explicit behavior for:
- Callee offline or unreachable.
- Invitation timeout.
- SDP negotiation failure.
- ICE connection failure.
- Temporary network loss.
- Signaling WebSocket disconnection.
- Duplicate, reordered or stale signaling messages.
- Caller cancellation during negotiation.
- Simultaneous hangup requests.
- App suspension or termination.
- Microphone permission denial or device removal.
- TURN unavailable or credentials expired.

Recovery policy:
- Distinguish temporary `disconnected` from terminal `failed` states.
- Apply bounded reconnection attempts with backoff.
- Use ICE restart when appropriate and supported.
- Reconnect signaling independently from media.
- Do not create a second PeerConnection while the previous connection is still active unless explicitly replacing it.
- Enforce an overall recovery deadline.
- On terminal failure, notify both participants and clean up all server-side state.
- Make cleanup safe to execute multiple times.

## 7. Security

- Require authenticated WebSocket sessions.
- Verify authorization for every call and signaling message.
- Use TLS for signaling and secure WebRTC media transport.
- Issue short-lived TURN credentials.
- Apply rate limits to call creation and signaling.
- Validate message types, sizes and payload schemas.
- Prevent call ID enumeration from granting access.
- Avoid logging SDP, ICE candidate addresses, authentication tokens or media content by default.
- Define retention and privacy rules for call metadata.
- Treat client-reported call state as untrusted input.

## 8. Testing Requirements

### Unit tests
- Call state transitions and transition rejection.
- Authorization and participant validation.
- Idempotency and duplicate requests.
- Timeout and cancellation races.
- Concurrent hangup and cleanup.
- Message validation and error mapping.

### Integration tests
- Two clients exchanging signaling messages.
- Complete offer/answer and ICE exchange.
- STUN-assisted direct connectivity.
- TURN-relayed connectivity.
- Signaling reconnect while a call is active.
- Backend restart and stale call cleanup.
- Expired TURN credentials and unavailable TURN.

### End-to-end tests
- Successful call, accept, reject, cancel and hangup.
- Both participants terminating simultaneously.
- Connection timeout.
- Network loss and recovery.
- Microphone permission failure.
- ICE failure and terminal cleanup.

### Network and load tests
- Symmetric and restrictive NAT scenarios.
- UDP blocked, high latency, jitter and packet loss.
- Many concurrent calls and WebSocket connections.
- Slow clients and signaling backpressure.
- Resource cleanup after repeated call cycles.

Tests must distinguish environments where direct P2P is possible from those requiring TURN. Do not assume all NAT combinations permit direct connectivity.

## 9. Observability

Use structured logs, Prometheus metrics and Grafana dashboards.

Required metrics:
- Call attempts, accepted calls and terminal outcomes by reason.
- Connection success rate.
- Call setup duration: p50, p95 and p99.
- Direct versus TURN-relayed connection ratio.
- ICE failures, restarts and reconnection success rate.
- RTT, jitter, packet loss and audio concealment where available.
- Active calls, active WebSockets and signaling message errors.
- Backend CPU, memory, goroutines and request latency.
- TURN allocation count, traffic and errors.

Use `call_id` as a correlation identifier in logs and traces. Do not use user IDs or call IDs as unbounded-cardinality Prometheus labels.

## 10. Code Quality and Engineering Constraints

- Keep call lifecycle, signaling transport and WebRTC client logic separate.
- Define explicit interfaces at genuine external boundaries.
- Use context cancellation and bounded timeouts in Go.
- Avoid holding locks during network I/O.
- Protect shared call state against concurrent mutation.
- Prevent goroutine leaks and unbounded message queues.
- Make resource ownership and cleanup explicit.
- Use typed errors and centralized error mapping.
- Keep configuration externalized and validated at startup.
- Document protocol schemas and state transitions.
- Run `go test ./...`, `go vet ./...`, `golangci-lint` and race detection in CI.
- Use deterministic tests for lifecycle logic and real network tests for WebRTC connectivity.
- Do not introduce microservices, distributed locks or complex abstractions without demonstrated need.

## 11. Definition of Done

The implementation is complete when:
- Two authorized users can establish a one-to-one audio call.
- Direct media transport works when network conditions permit.
- TURN relay works when direct connectivity is unavailable.
- Invalid transitions and unauthorized signaling are rejected.
- Duplicate requests do not duplicate side effects.
- Calls terminate and release resources reliably.
- Network and signaling failures produce bounded, observable outcomes.
- Critical state transitions and recovery paths have automated tests.
- CI checks pass.
- Metrics allow operators to identify setup failures, media quality problems and TURN dependency.

## 12. Implementation Order

1. Define protocol schemas and call state machine.
2. Implement Go call management and WebSocket signaling.
3. Implement client-side WebRTC offer/answer and ICE exchange.
4. Establish calls on a simple network.
5. Configure STUN and validate direct connectivity.
6. Configure TURN and validate relay connectivity.
7. Implement cancellation, timeouts, cleanup and recovery.
8. Add metrics, dashboards and structured logs.
9. Add network-condition tests and load tests.
10. Validate security and production deployment.
