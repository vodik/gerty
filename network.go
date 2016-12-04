package main

import (
	"fmt"
	"github.com/krolaw/dhcp4"
	"github.com/vishvananda/netlink"
	"net"
	"time"
)

type networkConfig struct {
	Subnet string
}

type DHCPHandler struct {
	serverId      net.IP
	yIAddr        net.IP
	leaseDuration time.Duration
	options       dhcp4.Options
}

func CreateNetwork(config networkConfig) (tap *netlink.Tuntap, err error) {
	// ip, ipnet, err := net.ParseCIDR("10.10.5.3/30")
	// if err != nil {
	// 	return
	// }

	la := netlink.NewLinkAttrs()
	la.Name = "tap0"

	tap = &netlink.Tuntap{
		LinkAttrs: la,
		Mode:      netlink.TUNTAP_MODE_TAP,
		Flags:     0,
	}

	if err = netlink.LinkAdd(tap); err != nil {
		return
	}

	addr, err := netlink.ParseAddr(config.Subnet)
	if err != nil {
		return
	}

	if err = netlink.AddrAdd(tap, addr); err != nil {
		return
	}

	if err = netlink.LinkSetUp(tap); err != nil {
		return
	}

	go DHCPServer(tap.Name)
	return tap, nil
}

func DHCPServer(iface string) {
	ip := net.IP{172, 18, 47, 1}
	handler := &DHCPHandler{
		serverId:      ip,
		yIAddr:        dhcp4.IPAdd(ip, 1),
		leaseDuration: 2 * time.Hour,
		options: dhcp4.Options{
			dhcp4.OptionSubnetMask:       []byte{255, 255, 255, 252},
			dhcp4.OptionRouter:           []byte(ip),
			dhcp4.OptionDomainNameServer: []byte{8, 8, 8, 8}},
	}

	fmt.Printf("Starting DHCP!\n")
	dhcp4.ListenAndServeIf(iface, handler)
}

func (h *DHCPHandler) ServeDHCP(p dhcp4.Packet, msgType dhcp4.MessageType, options dhcp4.Options) (d dhcp4.Packet) {
	switch msgType {
	case dhcp4.Discover:
		fmt.Printf("Got Discover...\n")
		return dhcp4.ReplyPacket(p, dhcp4.Offer, h.serverId, h.yIAddr, h.leaseDuration,
			h.options.SelectOrderOrAll(options[dhcp4.OptionParameterRequestList]))

	case dhcp4.Request:
		if server, ok := options[dhcp4.OptionServerIdentifier]; ok && !net.IP(server).Equal(h.serverId) {
			return nil // Message not for this dhcp server
		}

		reqIP := net.IP(options[dhcp4.OptionRequestedIPAddress])
		if reqIP == nil {
			reqIP = net.IP(p.CIAddr())
		}

		if len(reqIP) == 4 && !reqIP.Equal(net.IPv4zero) {
			fmt.Printf("Sending ACK!\n")
			return dhcp4.ReplyPacket(p, dhcp4.ACK, h.serverId, reqIP, h.leaseDuration,
				h.options.SelectOrderOrAll(options[dhcp4.OptionParameterRequestList]))
		}

		fmt.Printf("Sending NACK!\n")
		return dhcp4.ReplyPacket(p, dhcp4.NAK, h.serverId, nil, 0, nil)
	}

	return nil
}
