package main

import (
	"fmt"
	"github.com/pions/webrtc/pkg/rtp"
	"net"
)

func main() {
	srcAddr := "127.0.0.1:5000"
	dstAddr := "127.0.0.1:5001"

	// 1. Создаем UDP-сервер для прослушивания входящего потока от FFmpeg
	lAddr, _ := net.ResolveUDPAddr("udp", srcAddr)
	conn, err := net.ListenUDP("udp", lAddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	// 2. Создаем UDP-сокет для отправки (без жесткой привязки Dial)
	rAddr, _ := net.ResolveUDPAddr("udp", dstAddr)
	// Отправляем пакеты с любого свободного порта
	sendConn, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	defer sendConn.Close()

	fmt.Printf("🚀 Proxy started: %s -> %s\n", srcAddr, dstAddr)

	buf := make([]byte, 2048)
	packet := &rtp.Packet{}

	for {
		// Читаем пакет от FFmpeg
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("Read error:", err)
			continue
		}

		// Пытаемся распарсить заголовок RTP для статистики
		if err := packet.Unmarshal(buf[:n]); err == nil {
			fmt.Printf("📦 Packet: Seq=%d, TS=%d, Size=%d\n",
				packet.SequenceNumber, packet.Timestamp, n)
		}

		// Отправляем пакет в сторону VLC
		_, err = sendConn.WriteToUDP(buf[:n], rAddr)
		if err != nil {
			fmt.Println("Write error:", err)
		}
	}
}
