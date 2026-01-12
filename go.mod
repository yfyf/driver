module github.com/dividat/driver

go 1.18

require (
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/cskr/pubsub v1.0.2
	github.com/denisbrodbeck/machineid v1.0.1
	github.com/ebfe/scard v0.0.0-20190212122703-c3d1b1916a95
	github.com/gorilla/websocket v1.4.2
	github.com/kardianos/service v1.2.0

	// `libp2p/zeroconf` is a fork of `grandcat/zeroconf`, which we previously used.
	// This fork includes some stability improvements and bug fixes that are absent
	// in the grandcat version. However, it is libp2p's internal maintenance fork,
	// which while being public, does not accept community contributions.
	// Both projects are dormant at the moment, but we might want to re-evaluate this
	// dependency choice as these projects evolve in the future.
	github.com/libp2p/zeroconf/v2 v2.2.0
	github.com/pin/tftp v2.1.0+incompatible
	github.com/sirupsen/logrus v1.8.1
	go.bug.st/serial v1.6.1
)

require (
	github.com/creack/goselect v0.1.2 // indirect
	github.com/miekg/dns v1.1.43 // indirect
	golang.org/x/net v0.0.0-20210423184538-5f58ad60dda6 // indirect
	golang.org/x/sys v0.19.0 // indirect
)

replace go.bug.st/serial => github.com/yfyf/go-serial v1.6.4-bcddevicetest2
