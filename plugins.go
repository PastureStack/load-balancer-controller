package main

import (
	// controllers
	_ "github.com/PastureStack/load-balancer-controller/controller/rancher"

	//providers
	_ "github.com/PastureStack/load-balancer-controller/provider/haproxy"
)
