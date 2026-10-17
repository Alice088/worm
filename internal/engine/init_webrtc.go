package engine

import (
	"time"

	"github.com/pion/webrtc/v4"
)

func initWebRTC() *webrtc.API {
	var m webrtc.MediaEngine
	m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio)

	var s webrtc.SettingEngine
	s.SetICETimeouts(6*time.Second, 20*time.Second, 2*time.Second) // keepalive чаще → NAT не закрывается

	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(&m),
		webrtc.WithSettingEngine(s),
	)

	return api
}
