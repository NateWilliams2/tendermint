package main

import "net"

// incrIP increments returns the given IP address incremented by 1.
func incrIP(ip net.IP) net.IP {
	c := make([]byte, len(ip))
	copy(c, ip)
	ip = net.IP(c)

	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] > 0 {
			break
		}
	}
	return ip
}
