module github.com/k3s-io/nautilus-tee

go 1.21

require github.com/sirupsen/logrus v1.9.3

require (
	github.com/stretchr/testify v1.8.2 // indirect
	golang.org/x/sys v0.10.0 // indirect
)

// K3s-DaaS 로컬 패키지 참조
replace github.com/k3s-io/k3s => ../k3s-daas/pkg-reference
