package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

func main() {
	var (
		cfg           cfg
		flagInterface string
	)

	flag.StringVar(&flagInterface, "i", "", "Network interface we want to search")
	flag.IntVar(&cfg.port, "p", 0, "Port we want to find")
	flag.IntVar(&cfg.maxConnections, "c", 40, "Max number of concurrent connections to use")
	flag.DurationVar(&cfg.timeout, "t", time.Second, "Connect timeout")
	flag.BoolVar(&cfg.includeSelf, "self", false, "Include any ports found on this machine")
	flag.Parse()

	interfaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}
	for _, i := range interfaces {
		if i.Name == flagInterface {
			cfg.netIf = &i
			break
		}
	}
	if !cfg.valid() {
		flag.PrintDefaults()
		os.Exit(1)
	}

	queue := make(chan *dialItem, cfg.maxConnections-1)
	result := make(chan *dialItem, cap(queue))

	addrs, err := cfg.netIf.Addrs()
	if err != nil {
		panic(err)
	}

	var queueWg sync.WaitGroup
	for _, addr := range addrs {
		if addr.Network() != "ip+net" {
			continue
		}
		ip, ipNet, err := net.ParseCIDR(addr.String())
		if err != nil {
			continue
		}
		if ip.To4() == nil {
			continue
		}
		queueWg.Add(1)
		go func() {
			for dialIP := ip.Mask(ipNet.Mask); ipNet.Contains(dialIP); incIP4(dialIP) {
				if !cfg.includeSelf && ip.Equal(dialIP) {
					continue
				}
				queueWg.Add(1)
				item := &dialItem{
					ip:   make(net.IP, len(dialIP)),
					port: cfg.port,
				}
				copy(item.ip, dialIP)
				queue <- item
				queueWg.Done()
			}
			queueWg.Done()
		}()
	}
	go func() {
		queueWg.Wait()
		close(queue)
	}()

	go func() {
		var resultWg sync.WaitGroup
		for i := range queue {
			resultWg.Add(1)
			go func(item *dialItem) {
				conn, connErr := net.DialTimeout("tcp", item.String(), cfg.timeout)
				if conn != nil {
					conn.Close()
				}
				if connErr != nil {
					item.error = connErr.Error()
				}
				result <- item
				resultWg.Done()
			}(i)
		}
		resultWg.Wait()
		close(result)
	}()

	for di := range result {
		if di.error == "" {
			fmt.Fprintf(os.Stdout, "%s\n", di)
		} else {
			fmt.Fprintf(os.Stderr, "%s %s\n", di, di.error)
		}
	}

}

type cfg struct {
	netIf          *net.Interface
	port           int
	maxConnections int
	timeout        time.Duration
	includeSelf    bool
}

func (c *cfg) valid() bool {
	if c.netIf == nil {
		return false
	}
	if c.port < 0 || c.port > 65536 {
		return false
	}
	if c.maxConnections < 1 {
		return false
	}
	if c.timeout < 1 {
		return false
	}
	return true
}

func incIP4(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

type dialItem struct {
	ip    net.IP
	port  int
	error string
}

func (i *dialItem) String() string {
	return fmt.Sprintf("%s:%d", i.ip, i.port)
}
