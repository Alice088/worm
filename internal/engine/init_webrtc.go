package engine

import (
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/Alice088/worm/internal/audio"
)

func newAPI(tune func(*webrtc.SettingEngine)) *webrtc.API {
	var media webrtc.MediaEngine
	_ = media.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  "audio/L16",
			ClockRate: audio.SampleRate,
			Channels:  audio.Channels,
		},
		PayloadType: l16PayloadType,
	}, webrtc.RTPCodecTypeAudio)

	var settings webrtc.SettingEngine
	settings.SetICETimeouts(10*time.Second, 30*time.Second, 2*time.Second)
	if tune != nil {
		tune(&settings)
	}

	return webrtc.NewAPI(
		webrtc.WithMediaEngine(&media),
		webrtc.WithSettingEngine(settings),
	)
}
