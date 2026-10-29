package engine

import (
	"encoding/json"

	"github.com/pion/webrtc/v4"

	"github.com/Alice088/worm/internal/protocol"
)

func marshal(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func errString(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}

func toICEServers(list []protocol.ICEServer) []webrtc.ICEServer {
	out := make([]webrtc.ICEServer, 0, len(list))
	for _, s := range list {
		out = append(out, webrtc.ICEServer{
			URLs:       append([]string(nil), s.URLs...),
			Username:   s.Username,
			Credential: s.Credential,
		})
	}
	return out
}

func webrtcCandidateInit(p protocol.ICECandidatePayload) webrtc.ICECandidateInit {
	return webrtc.ICECandidateInit{
		Candidate:        p.Candidate,
		SDPMid:           p.SDPMid,
		SDPMLineIndex:    p.SDPMLineIndex,
		UsernameFragment: p.UsernameFragment,
	}
}
