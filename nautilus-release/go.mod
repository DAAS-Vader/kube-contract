module github.com/k3s-io/nautilus-tee

go 1.21

require (
	github.com/k3s-io/k3s v1.28.3-0.20230919131847-6330a5b49cfe
	github.com/sirupsen/logrus v1.9.3
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/apiserver v0.28.2
)

require (
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.10.0 // indirect
	golang.org/x/sys v0.10.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

// K3s-DaaS 로컬 패키지 참조
replace github.com/k3s-io/k3s => ../k3s-daas/pkg-reference
