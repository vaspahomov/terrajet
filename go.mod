module github.com/crossplane/terrajet

go 1.16

require (
	github.com/crossplane/crossplane-runtime v0.15.1-0.20211004150827-579c1833b513
	github.com/fatih/camelcase v1.0.0
	github.com/go-openapi/spec v0.19.5 // indirect
	github.com/golang/mock v1.6.0
	github.com/google/go-cmp v0.5.6
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/hashicorp/terraform-plugin-sdk v1.17.2
	github.com/hashicorp/terraform-plugin-sdk/v2 v2.7.0
	github.com/iancoleman/strcase v0.2.0
	github.com/json-iterator/go v1.1.12
	github.com/kr/pty v1.1.5 // indirect
	github.com/muvaf/typewriter v0.0.0-20210910160850-80e49fe1eb32
	github.com/pkg/errors v0.9.1
	github.com/spf13/afero v1.8.0
	github.com/stretchr/objx v0.2.0 // indirect
	go.etcd.io/etcd v0.5.0-alpha.5.0.20200910180754-dd1b699fc489 // indirect
	golang.org/x/tools v0.1.6-0.20210820212750-d4cc65f0b2ff
	k8s.io/api v0.23.0
	k8s.io/apimachinery v0.23.0
	k8s.io/utils v0.0.0-20210930125809-cb0fa318a74b
	sigs.k8s.io/controller-runtime v0.11.0
)

// This is a temporary workaround until https://github.com/crossplane/terrajet/issues/131
// is resolved. We basically need this just to be able to import both v1 and v2
// versions of terraform plugin sdk in order to do a schema conversion for
// Terraform providers still using v1 sdk.
replace (
	github.com/crossplane/crossplane-runtime => github.com/vaspahomov/crossplane-runtime v0.14.1-0.20220202070154-70fc88a4d127
	github.com/hashicorp/terraform-plugin-sdk => github.com/turkenh/terraform-plugin-sdk v1.17.2-patch1
)
