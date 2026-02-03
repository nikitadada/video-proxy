package main

import (
	"encoding/json"
	"fmt"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"net"
	"net/http"
)

// Глобальный трек, чтобы все клиенты могли подключиться к одному источнику
var videoTrack *webrtc.TrackLocalStaticRTP

func main() {
	var err error

	// 1. Инициализируем видеотрек ОДИН РАЗ при старте
	videoTrack, err = webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion",
	)
	if err != nil {
		panic(err)
	}

	// 2. Запускаем UDP-приемник в отдельной горутине (Ingest)
	// Он будет работать вечно, даже если FFmpeg перезапускается
	go startUDPListener()

	// 3. Раздача статики
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// 4. Signaling API
	http.HandleFunc("/webrtc", webrtcHandler)

	fmt.Println("Сервер: http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

func startUDPListener() {
	udpAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:5000")
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		fmt.Printf("Ошибка: не удалось занять порт 5000: %v\n", err)
		return
	}
	defer conn.Close()

	fmt.Println("UDP-приемник готов на порту 5000 (жду FFmpeg)")

	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			fmt.Printf("Ошибка чтения UDP: %v\n", err)
			continue
		}

		// Распаковываем RTP пакет
		packet := &rtp.Packet{}
		if err := packet.Unmarshal(buf[:n]); err == nil {
			// Транслируем всем подключенным WebRTC клиентам
			videoTrack.WriteRTP(packet)
		}
	}
}

func webrtcHandler(w http.ResponseWriter, r *http.Request) {
	var offer webrtc.SessionDescription
	if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
		return
	}

	// Настройка соединения
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	})
	if err != nil {
		return
	}

	// Добавляем наш глобальный трек к этому конкретному соединению
	if _, err = peerConnection.AddTrack(videoTrack); err != nil {
		return
	}

	// Стандартный Handshake
	peerConnection.SetRemoteDescription(offer)
	answer, _ := peerConnection.CreateAnswer(nil)

	// Ждем сбора кандидатов (Gathering)
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	peerConnection.SetLocalDescription(answer)
	<-gatherComplete

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(peerConnection.LocalDescription())

	fmt.Printf("Новый зритель подключен! (Remote IP: %s)\n", r.RemoteAddr)
}
