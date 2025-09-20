module github.com/k3s-io/nautilus-tee

go 1.21

require (
	github.com/gorilla/websocket v1.5.0
	github.com/sirupsen/logrus v1.9.3
	k8s.io/apiserver v0.28.0
)

require golang.org/x/sys v0.10.0 // indirect

// K3s-DaaS 로컬 패키지 참조
replace github.com/k3s-io/k3s => ../k3s-daas/pkg-reference
