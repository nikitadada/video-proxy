package main

import (
	"fmt"
	"github.com/pion/rtcp"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type SignalMessage struct {
	Type      string                   `json:"type"`
	SDP       string                   `json:"sdp,omitempty"`
	Candidate *webrtc.ICECandidateInit `json:"candidate,omitempty"`
	PeerID    string                   `json:"peerId,omitempty"`
}

type Peer struct {
	id    string
	pc    *webrtc.PeerConnection
	ws    *websocket.Conn
	track *webrtc.TrackLocalStaticRTP
}

type Room struct {
	sync.RWMutex
	peers map[string]*Peer
}

var (
	roomsMu sync.Mutex
	rooms   = make(map[string]*Room)
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})
	http.HandleFunc("/ws", handleWebSocket)

	fmt.Println("🚀 SFU Server LIVE: http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 1. Мгновенный ответ браузеру (чтобы статус стал 101)
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	defer ws.Close()
	log.Println("✅ WebSocket подключен")

	roomID := r.URL.Query().Get("room")
	peerID := fmt.Sprintf("peer-%d", time.Now().UnixNano())

	// 2. Создаем PeerConnection
	pc, _ := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	})

	// ОСТАВЛЯЕМ ТОЛЬКО ОДИН DEFER В КОНЦЕ
	room := getOrCreateRoom(roomID)
	peer := &Peer{id: peerID, pc: pc, ws: ws}

	defer func() {
		fmt.Printf("❌ %s покинул комнату\n", peerID)
		room.Lock()
		delete(room.peers, peerID)
		leaveMsg := SignalMessage{Type: "peer-left", PeerID: peerID}
		for _, p := range room.peers {
			_ = p.ws.WriteJSON(leaveMsg)
		}
		room.Unlock()
		pc.Close()
	}()

	// 3. Настраиваем отправку кандидатов (Trickle ICE)
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		candidate := c.ToJSON()
		_ = ws.WriteJSON(SignalMessage{Type: "candidate", Candidate: &candidate})
	})

	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("🎥 Получен трек от %s", peerID)
		localTrack, _ := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, peerID, peerID)
		peer.track = localTrack

		// Запрашиваем ключевой кадр, чтобы видео появилось сразу
		go func() {
			ticker := time.NewTicker(time.Second * 3)
			for range ticker.C {
				_ = pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(remoteTrack.SSRC())}})
			}
		}()

		room.Lock()
		for _, p := range room.peers {
			if p.id != peerID {
				p.pc.AddTrack(localTrack)
				offer, _ := p.pc.CreateOffer(nil)
				_ = p.pc.SetLocalDescription(offer)
				_ = p.ws.WriteJSON(SignalMessage{Type: "offer", SDP: offer.SDP})
			}
		}
		room.Unlock()

		for {
			packet, _, err := remoteTrack.ReadRTP()
			if err != nil {
				return
			}
			_ = localTrack.WriteRTP(packet)
		}
	})

	// Подписка новичка на старичков
	room.Lock()
	for _, p := range room.peers {
		if p.track != nil {
			pc.AddTrack(p.track)
		}
	}
	room.peers[peerID] = peer
	room.Unlock()

	for {
		var msg SignalMessage
		if err := ws.ReadJSON(&msg); err != nil {
			break
		}

		switch msg.Type {
		// ... внутри handleWebSocket в блоке switch msg.Type ...
		case "offer":
			// 1. ПРОВЕРКА: Если это первый оффер от новичка, подписываем его на всех
			// Мы проверяем количество сендеров: если их мало, значит мы еще не подписывали его
			if len(pc.GetSenders()) <= 1 {
				room.RLock()
				for id, p := range room.peers {
					if id != peerID && p.track != nil {
						log.Printf("🔌 Подписываем новичка %s на трек участника %s", peerID, id)
						pc.AddTrack(p.track)
					}
				}
				room.RUnlock()
			}

			// 2. Устанавливаем Offer и создаем Answer (теперь в Answer будут ВСЕ треки комнаты)
			pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: msg.SDP})
			answer, _ := pc.CreateAnswer(nil)

			// Ждем сбора кандидатов (Gathering) для стабильности
			gatherComplete := webrtc.GatheringCompletePromise(pc)
			pc.SetLocalDescription(answer)
			<-gatherComplete

			ws.WriteJSON(SignalMessage{Type: "answer", SDP: pc.LocalDescription().SDP})

		case "answer":
			_ = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: msg.SDP})
		case "candidate":
			if msg.Candidate != nil {
				_ = pc.AddICECandidate(*msg.Candidate)
			}
		}
	}
}

func getOrCreateRoom(id string) *Room {
	roomsMu.Lock()
	defer roomsMu.Unlock()
	if r, ok := rooms[id]; ok {
		return r
	}
	rooms[id] = &Room{peers: make(map[string]*Peer)}
	return rooms[id]
}
