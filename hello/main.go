package main

import (
	"fmt"
	"math/rand"
	"net"
	"time"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

func main() {
	target := "157.240.226.35"
	maxHops := 15
	timeout := 1 * time.Second

	ipAddr, err := net.ResolveIPAddr("ip4", target) // resolve o IP target
	if err != nil {
		fmt.Println("Erro ao resolver IP:", err)
		return
	}

	conn, err := net.ListenPacket("ip4:icmp", "") // onde vou ouvir os pacotes (definir o IP)
	if err != nil {
		fmt.Println("Erro ao escutar ICMP:", err)
		return
	}
	defer conn.Close() // agenda o fechamento do conn

	pconn := ipv4.NewPacketConn(conn) // envelopa a conexão em um PacketConn o que permite mais funções do IP
	defer pconn.Close()

	fmt.Println("Início do traceroute")
	for ttl := 1; ttl <= maxHops; ttl++ { // começa a enviar pacotes com TTL 1 e vai até o máximo de hops
		err := pconn.SetTTL(ttl)
		if err != nil {
			fmt.Printf("%d * (erro ao configurar TTL: %v)\n", ttl, err)
			continue
		}

		message := icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   rand.Intn(65535), // 16 bits
				Seq:  ttl,
				Data: []byte("traceroute"),
			},
		}
		writeBytes, err := message.Marshal(nil) // serializa a mensagem e calcula checkSum
		if err != nil {
			fmt.Println("Erro ao montar pacote:", err)
			return
		}

		start := time.Now()
		_, err = pconn.WriteTo(writeBytes, nil, ipAddr) // envia a mensagem para target
		if err != nil {
			fmt.Printf("%2d * (erro ao enviar)\n", ttl)
			continue
		}

		reply := make([]byte, 1500) // 1500 é tamanho máximo para a resposta
		if err := pconn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			fmt.Printf("%2d * (erro ao configurar timeout: %v)\n", ttl, err)
		}

		n, _, peer, err := pconn.ReadFrom(reply)
		// fmt.Println(n, cm, peer, err)
		if err != nil {
			fmt.Println("Timeout", ttl, n, peer, err)
			// continue
		}

		duration := time.Since(start)
		rm, err := icmp.ParseMessage(1, reply[:n])
		if err != nil {
			fmt.Println("Erro ao interpretar resposta:", err)
			continue
		}

		switch rm.Type {
		case ipv4.ICMPTypeTimeExceeded:
			fmt.Printf("%2d  %-15s  %v\n", ttl, peer, duration)
		case ipv4.ICMPTypeEchoReply:
			fmt.Printf("TTL: %d  %-15s  %v\n", ttl, peer, duration)
			fmt.Println("Destino alcançado.")
			return
		default:
			fmt.Printf("TTL: %d  %-15s  %v (tipo %v)\n", ttl, peer, duration, rm)
		}
	}
}
