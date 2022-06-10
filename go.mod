module github.com/harness/lite-engine

go 1.17

replace github.com/docker/docker => github.com/docker/engine v17.12.0-ce-rc1.0.20200309214505-aa6a9891b09c+incompatible

require (
	github.com/cenkalti/backoff/v4 v4.1.2
	github.com/docker/distribution v2.8.0+incompatible
	github.com/docker/docker v1.4.2-0.20190924003213-a8608b5b67c7
	// this is fake as we are using github.com/docker/engine, this makes the security warning go away
	github.com/docker/go-connections v0.4.0
	github.com/drone/drone-go v1.7.1
	github.com/drone/runner-go v1.12.0
	github.com/go-chi/chi v1.5.4
	github.com/gofrs/uuid v4.0.0+incompatible
	github.com/golang/mock v1.6.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/joho/godotenv v1.4.0
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/linkedin/goavro/v2 v2.10.1
	github.com/mholt/archiver/v3 v3.5.1
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.7.0
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/andybalholm/brotli v1.0.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dsnet/compress v0.0.2-0.20210315054119-f66993602bf5 // indirect
	github.com/golang/snappy v0.0.2 // indirect
	github.com/klauspost/compress v1.11.13 // indirect
	github.com/klauspost/pgzip v1.2.5 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/nwaples/rardecode v1.1.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.2 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/ulikunitz/xz v0.5.9 // indirect
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/99designs/httpsignatures-go v0.0.0-20170731043157-88528bf4ca7e // indirect
	github.com/Azure/go-ansiterm v0.0.0-20210617225240-d185dfc1b5a1 // indirect
	github.com/Microsoft/go-winio v0.5.2 // indirect
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751 // indirect
	github.com/alecthomas/units v0.0.0-20210927113745-59d0afb8317a // indirect
	github.com/bmatcuk/doublestar v1.1.1 // indirect
	github.com/buildkite/yaml v2.1.0+incompatible // indirect
	github.com/containerd/containerd v1.6.6 // indirect
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/drone/envsubst v1.0.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/gorilla/mux v1.7.4 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/mattn/go-zglob v0.0.3
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/natessilva/dag v0.0.0-20180124060714-7194b8dcc5c4 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.3-0.20211202183452-c5a74bcca799 // indirect
	golang.org/x/net v0.0.0-20220403103023-749bd193bc2b // indirect
	golang.org/x/sys v0.0.0-20220412211240-33da011f77ad // indirect
	google.golang.org/genproto v0.0.0-20220405205423-9d709892a2bf // indirect
	google.golang.org/grpc v1.45.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gotest.tools v2.2.0+incompatible // indirect
)
