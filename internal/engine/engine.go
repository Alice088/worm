package engine

import "github.com/pion/webrtc/v4"

type Engine struct {
	webRTC *webrtc.API
	peerConn []*webrtc.PeerConnection
}

func NewEngine() *Engine {
	var e = &Engine{}

	e.webRTC = initWebRTC()

	pc, _ := e.webRTC.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	})

	e.peerConn = append(e.peerConn, pc)

	return e
}
