module github.com/harness/lite-engine

go 1.17

replace github.com/docker/docker => github.com/docker/engine v17.12.0-ce-rc1.0.20200309214505-aa6a9891b09c+incompatible

require (
	github.com/cenkalti/backoff/v4 v4.1.2
	github.com/docker/distribution v2.7.1+incompatible
	github.com/docker/docker v0.0.1
	github.com/docker/go-connections v0.4.0
	github.com/drone/drone-go v1.7.1
	github.com/drone/runner-go v1.11.0
	github.com/go-chi/chi v1.5.4
	github.com/gofrs/uuid v4.0.0+incompatible
	github.com/hashicorp/go-multierror v1.1.1
	github.com/joho/godotenv v1.4.0
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.6.1
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/opencontainers/image-spec v1.0.2 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c // indirect
)

require (
	github.com/99designs/httpsignatures-go v0.0.0-20170731043157-88528bf4ca7e // indirect
	github.com/Microsoft/go-winio v0.4.17 // indirect
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751 // indirect
	github.com/alecthomas/units v0.0.0-20210927113745-59d0afb8317a // indirect
	github.com/bmatcuk/doublestar v1.1.1 // indirect
	github.com/buildkite/yaml v2.1.0+incompatible // indirect
	github.com/containerd/containerd v1.5.8 // indirect
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/drone/envsubst v1.0.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.0 // indirect
	github.com/gorilla/mux v1.7.4 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/mattn/go-zglob v0.0.3
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/natessilva/dag v0.0.0-20180124060714-7194b8dcc5c4 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	golang.org/x/net v0.0.0-20210226172049-e18ecbb05110 // indirect
	golang.org/x/sys v0.0.0-20210426230700-d19ff857e887 // indirect
	google.golang.org/genproto v0.0.0-20201110150050-8816d57aaa9a // indirect
	google.golang.org/grpc v1.33.2 // indirect
)
