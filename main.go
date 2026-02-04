package main

import (
	"encoding/json"
	"fmt"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	http.HandleFunc("/webrtc", echoHandler)

	fmt.Println("🚀 Echo-server started at http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

func echoHandler(w http.ResponseWriter, r *http.Request) {
	var offer webrtc.SessionDescription
	json.NewDecoder(r.Body).Decode(&offer)

	peerConnection, _ := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	})

	// Создаем трек заранее
	localTrack, _ := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion",
	)
	peerConnection.AddTrack(localTrack)

	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// Читаем RTCP, чтобы сервер знал о состоянии сети (важно для Chrome)
		go func() {
			for {
				if _, _, err := receiver.ReadRTCP(); err != nil {
					return
				}
			}
		}()

		// Запрашиваем ключевой кадр (FIR + PLI)
		go func() {
			ticker := time.NewTicker(time.Second * 1)
			for range ticker.C {
				_ = peerConnection.WriteRTCP([]rtcp.Packet{
					&rtcp.PictureLossIndication{MediaSSRC: uint32(remoteTrack.SSRC())},
					&rtcp.FullIntraRequest{MediaSSRC: uint32(remoteTrack.SSRC())},
				})
			}
		}()

		for {
			packet, _, err := remoteTrack.ReadRTP()
			if err != nil {
				return
			}
			// Пересылка
			localTrack.WriteRTP(packet)
		}
	})

	peerConnection.SetRemoteDescription(offer)
	answer, _ := peerConnection.CreateAnswer(nil)
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	peerConnection.SetLocalDescription(answer)
	<-gatherComplete

	json.NewEncoder(w).Encode(peerConnection.LocalDescription())
}
