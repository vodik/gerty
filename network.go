package main

import (
	"fmt"
	"net"
	"time"

	"github.com/krolaw/dhcp4"
	"github.com/vishvananda/netlink"
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

func CreateNetwork(subnet string) (tap *netlink.Tuntap, err error) {
	ip, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return
	}

	serverId := dhcp4.IPAdd(ip, 1)
	if !ipnet.Contains(serverId) {
		panic("serverId invalid")
	}

	tapnet := &net.IPNet{IP: serverId, Mask: ipnet.Mask}

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

	addr := &netlink.Addr{IPNet: tapnet}
	if err = netlink.AddrAdd(tap, addr); err != nil {
		return
	}

	if err = netlink.LinkSetUp(tap); err != nil {
		return
	}

	fmt.Printf("DHCP server started on %s for %s\n", tap.Name, ipnet)
	go DHCPServer(tap.Name, tapnet)
	return tap, nil
}

func DHCPServer(iface string, network *net.IPNet) {
	ip := network.IP
	handler := &DHCPHandler{
		serverId:      ip,
		yIAddr:        dhcp4.IPAdd(ip, 1),
		leaseDuration: 2 * time.Hour,
		options: dhcp4.Options{
			dhcp4.OptionSubnetMask:       []byte{255, 255, 255, 252},
			dhcp4.OptionRouter:           []byte(ip),
			dhcp4.OptionDomainNameServer: []byte{8, 8, 8, 8}},
	}

	dhcp4.ListenAndServeIf(iface, handler)
}

func (h *DHCPHandler) ServeDHCP(p dhcp4.Packet, msgType dhcp4.MessageType, options dhcp4.Options) (d dhcp4.Packet) {
	switch msgType {
	case dhcp4.Discover:
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
			fmt.Printf("Assigning %s to VM\n", reqIP)
			return dhcp4.ReplyPacket(p, dhcp4.ACK, h.serverId, reqIP, h.leaseDuration,
				h.options.SelectOrderOrAll(options[dhcp4.OptionParameterRequestList]))
		}

		return dhcp4.ReplyPacket(p, dhcp4.NAK, h.serverId, nil, 0, nil)
	}

	return nil
}
