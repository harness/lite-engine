module github.com/harness/lite-engine

go 1.24.0

toolchain go1.24.12

require (
	github.com/bmatcuk/doublestar v1.3.4
	github.com/cenkalti/backoff/v4 v4.2.0
	github.com/docker/distribution v2.8.1+incompatible
	github.com/docker/docker v28.0.2+incompatible
	github.com/docker/go-connections v0.4.0
	github.com/drone/drone-go v1.7.1
	github.com/drone/runner-go v1.12.0
	github.com/go-chi/chi/v5 v5.0.8
	github.com/gofrs/uuid v4.4.0+incompatible
	github.com/golang/mock v1.6.0
	github.com/harness/ti-client v0.0.0-20260106231425-06bf65d965b0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/linkedin/goavro/v2 v2.12.0
	github.com/mattn/go-zglob v0.0.4
	github.com/mholt/archives v0.1.5
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.9.3
	github.com/stretchr/testify v1.11.1
	github.com/t-tomalak/logrus-easy-formatter v0.0.0-20190827215021-c074f06c5816
	golang.org/x/sync v0.17.0
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0
	github.com/dgryski/go-lttb v0.0.0-20230207170358-f8fc36cdbff1
	github.com/harness/godotenv/v2 v2.0.0
	github.com/harness/godotenv/v3 v3.0.1
	github.com/harness/godotenv/v4 v4.0.2
	github.com/shirou/gopsutil/v3 v3.23.5
	github.com/wings-software/dlite v1.0.0-rc.13
	golang.org/x/net v0.43.0
	golang.org/x/sys v0.35.0
)

require (
	github.com/99designs/httpsignatures-go v0.0.0-20170731043157-88528bf4ca7e // indirect
	github.com/Microsoft/go-winio v0.6.0 // indirect
	github.com/STARRY-S/zip v0.2.3 // indirect
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751 // indirect
	github.com/alecthomas/units v0.0.0-20211218093645-b94a6e3cc137 // indirect
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/bodgit/plumbing v1.3.0 // indirect
	github.com/bodgit/sevenzip v1.6.1 // indirect
	github.com/bodgit/windows v1.0.1 // indirect
	github.com/buildkite/yaml v2.1.0+incompatible // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/drone/envsubst v1.0.3 // indirect
	github.com/dsnet/compress v0.0.2-0.20230904184137-39efe44ab707 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/pgzip v1.2.6 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/mikelolasagasti/xz v1.0.1 // indirect
	github.com/minio/minlz v1.0.1 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/natessilva/dag v0.0.0-20180124060714-7194b8dcc5c4 // indirect
	github.com/nwaples/rardecode/v2 v2.2.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.0-rc2.0.20221005185240-3a7f492d3f1b // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/sorairolake/lzip-go v0.3.8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/tklauser/go-sysconf v0.3.11 // indirect
	github.com/tklauser/numcpus v0.6.0 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	github.com/yusufpapurcu/wmi v1.2.3 // indirect
	go.mongodb.org/mongo-driver v1.17.4 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.63.0 // indirect
	go.opentelemetry.io/otel v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.38.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/trace v1.38.0 // indirect
	go4.org v0.0.0-20230225012048-214862532bf5 // indirect
	golang.org/x/crypto v0.41.0 // indirect
	golang.org/x/exp v0.0.0-20220927162542-c76eaa363f9d // indirect
	golang.org/x/mod v0.27.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	golang.org/x/time v0.0.0-20220210224613-90d013bbcef8 // indirect
	golang.org/x/tools v0.36.0 // indirect
	google.golang.org/genproto v0.0.0-20230320184635-7606e756e683 // indirect
	gopkg.in/square/go-jose.v2 v2.6.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gotest.tools/v3 v3.5.2 // indirect
)
