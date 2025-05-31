package main

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"

	// "runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const (
	maxHops    = 30
	timeout    = 1500 // em milissegundos
	packetSize = 32
)

type IcmpEchoReply struct {
	Address  uint32
	Status   uint32
	Rtt      uint32
	DataSize uint16
	Reserved uint16
	Data     uintptr
	Options  [8]byte
}

type IPOptionInformation struct {
	Ttl         byte
	Tos         byte
	Flags       byte
	OptionsSize byte
	OptionsData *byte
}

func main() {
	host := "8.8.8.8"

	// if runtime.GOOS == "linux" {
	// 	linux()
	// }

	linux()

	ipAddr, err := net.ResolveIPAddr("ip4", host)
	if err != nil {
		fmt.Println("Erro ao resolver IP:", err)
		return
	}

	// Carrega a DLL e funções
	iphlpapi := syscall.NewLazyDLL("iphlpapi.dll")
	icmpCreateFile := iphlpapi.NewProc("IcmpCreateFile")
	icmpCloseHandle := iphlpapi.NewProc("IcmpCloseHandle")
	icmpSendEcho := iphlpapi.NewProc("IcmpSendEcho")

	handle, _, _ := icmpCreateFile.Call()
	if handle == 0 {
		fmt.Println("Erro ao abrir IcmpHandle")
		return
	}
	defer icmpCloseHandle.Call(handle)

	sendData := []byte("traceroute")
	reply := make([]byte, 4096)

	fmt.Printf("Traceroute para %s, %d saltos no máximo:\n", host, maxHops)

	for ttl := 1; ttl <= maxHops; ttl++ {
		opts := IPOptionInformation{
			Ttl:         byte(ttl),
			Tos:         0,
			Flags:       0,
			OptionsSize: 0,
			OptionsData: nil,
		}

		// start := time.Now()
		ret, _, err := icmpSendEcho.Call(
			handle,
			uintptr(binary.LittleEndian.Uint32(ipAddr.IP.To4())),
			uintptr(unsafe.Pointer(&sendData[0])),
			uintptr(len(sendData)),
			uintptr(unsafe.Pointer(&opts)),
			uintptr(unsafe.Pointer(&reply[0])),
			uintptr(len(reply)),
			uintptr(timeout),
		)

		// elapsed := time.Since(start)

		fmt.Println(ret, err)
		if ret == 0 {
			fmt.Printf("%2d  erro: %v\n", ttl, err)
			continue
		}
		if ret > 0 {
			r := (*IcmpEchoReply)(unsafe.Pointer(&reply[0]))
			hopIP := make(net.IP, 4)
			binary.BigEndian.PutUint32(hopIP, r.Address)
			fmt.Printf("%2d  %s  %.2f ms\n", ttl, hopIP.String(), float64(r.Rtt))

			currentHopIP := net.IPv4(
				byte(r.Address),
				byte(r.Address>>8),
				byte(r.Address>>16),
				byte(r.Address>>24),
			)
			fmt.Println(currentHopIP, ipAddr.IP)
			if currentHopIP.Equal(ipAddr.IP) {
				fmt.Println("Destino alcançado.")
				break
			}
		}
	}
}

func linux() {
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
		writeBytes, err := message.Marshal(nil) // serializa a mensagem
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
