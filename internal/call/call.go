package call

import "time"

type call struct {
	id          string
	caller      Party
	callee      Party
	state       State
	createdAt   time.Time
	deadline    time.Time
	connectedAt time.Time
	terminalAt  time.Time
	reason      string
	invite      string
	callerReady bool
	calleeReady bool
}

var allowedTransitions = map[State]map[State]bool{
	StateRinging: {
		StateAccepted:  true,
		StateRejected:  true,
		StateCancelled: true,
		StateTimeout:   true,
	},
	StateAccepted: {
		StateConnecting: true,
		StateConnected:  true,
		StateEnded:      true,
		StateCancelled:  true,
		StateFailed:     true,
	},
	StateConnecting: {
		StateConnected: true,
		StateEnded:     true,
		StateCancelled: true,
		StateFailed:    true,
	},
	StateConnected: {
		StateConnecting: true,
		StateEnded:      true,
		StateFailed:     true,
	},
}

func (c *call) participant(userID string) (Party, Role, bool) {
	switch userID {
	case "":
		return Party{}, "", false
	case c.caller.ID:
		return c.caller, RoleCaller, true
	case c.callee.ID:
		return c.callee, RoleCallee, true
	}
	return Party{}, "", false
}

func (c *call) transition(to State, now time.Time, connectionTimeout time.Duration) error {
	if !to.Valid() {
		return &StateError{From: c.state, To: to}
	}
	if c.state.Terminal() {
		return &StateError{From: c.state, To: to}
	}
	if to != c.state && !allowedTransitions[c.state][to] {
		return &StateError{From: c.state, To: to}
	}

	from := c.state
	c.state = to
	switch to {
	case StateAccepted:
		c.deadline = now.Add(connectionTimeout)
	case StateConnecting:
		if from != StateConnecting {
			c.deadline = now.Add(connectionTimeout)
		}
	case StateConnected:
		c.connectedAt = now
		c.deadline = time.Time{}
	case StateEnded, StateRejected, StateCancelled, StateTimeout, StateFailed:
		c.terminalAt = now
		c.deadline = time.Time{}
	}
	return nil
}

func (c *call) snapshot() Snapshot {
	return Snapshot{
		ID:          c.id,
		State:       c.state,
		Caller:      c.caller,
		Callee:      c.callee,
		CreatedAt:   c.createdAt,
		Deadline:    c.deadline,
		ConnectedAt: c.connectedAt,
		TerminalAt:  c.terminalAt,
		Reason:      c.reason,
		Invite:      c.invite,
	}
}
