package main

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

func main() {
	target := "8.8.8.8"
	timeout := 1 * time.Second
	maxHops := 15

	fmt.Printf("Traceroute para %s, %d saltos no máximo:\n\n", target, maxHops)
	ipAddr, err := net.ResolveIPAddr("ip4", target) // resolve o IP target
	if err != nil {
		fmt.Printf("Erro ao resolver %s, %s\n\n", target, err)
		return
	}

	if runtime.GOOS == "linux" {
		linux(ipAddr, timeout, maxHops)
	} else {
		windows(ipAddr, timeout, maxHops)
	}

}

func linux(ipAddr *net.IPAddr, timeout time.Duration, maxHops int) {
	conn, err := net.ListenPacket("ip4:icmp", "") // onde vou ouvir os pacotes (definir o IP)
	if err != nil {
		fmt.Println("Erro ao escutar ICMP:", err)
		return
	}
	defer conn.Close() // agenda o fechamento do conn

	pconn := ipv4.NewPacketConn(conn) // envelopa a conexão em um PacketConn o que permite mais funções do IP
	defer pconn.Close()

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
		if err != nil {
			fmt.Println("Timeout", ttl, n, peer, err)
			continue
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
			return
		case ipv4.ICMPTypeEchoReply:
			fmt.Printf("TTL: %d  %-15s  %v\n", ttl, peer, duration)
			fmt.Println("Destino alcançado")
			return
		default:
			fmt.Printf("TTL: %d  %-15s  %v (tipo %v)\n", ttl, peer, duration, rm)
			fmt.Println("Destino não alcançado")
		}
	}
}

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

func windows(ipAddr *net.IPAddr, timeout time.Duration, maxHops int) {
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
	reply := make([]byte, 1500)

	for ttl := 1; ttl <= maxHops; ttl++ {
		opts := IPOptionInformation{
			Ttl:         byte(ttl),
			Tos:         0,
			Flags:       0,
			OptionsSize: 0,
			OptionsData: nil,
		}

		ret, _, err := icmpSendEcho.Call(
			handle,
			uintptr(binary.BigEndian.Uint32(ipAddr.IP.To4())),
			uintptr(unsafe.Pointer(&sendData[0])),
			uintptr(len(sendData)),
			uintptr(unsafe.Pointer(&opts)),
			uintptr(unsafe.Pointer(&reply[0])),
			uintptr(len(reply)),
			uintptr(timeout),
		)

		if ret == 0 {
			fmt.Printf("%2d  erro: %v\n", ttl, err)
			continue
		}
		if ret > 0 {
			r := (*IcmpEchoReply)(unsafe.Pointer(&reply[0]))

			currentHopIP := net.IPv4(
				byte(r.Address),
				byte(r.Address>>8),
				byte(r.Address>>16),
				byte(r.Address>>24),
			)
			fmt.Printf("%d  %-20s %.2f ms\n", ttl, currentHopIP, float64(r.Rtt))
			if currentHopIP.Equal(ipAddr.IP) {
				fmt.Println("Destino alcançado")
				os.Exit(0)
			}
		}
	}
	fmt.Println("Destino não alcançado")
}
