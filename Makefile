CONTROLLER_GEN ?= go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.17.3
ENVTEST ?= go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: generate manifests setup-envtest build test lint fmt tidy

tidy:
	go mod tidy
	go mod download

generate:
	rm -f api/v1alpha1/zz_generated.deepcopy.go
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

manifests:
	$(CONTROLLER_GEN) crd:allowDangerousTypes=true \
		rbac:roleName=controller-manager-role \
		paths="./api/..." \
		output:crd:artifacts:config=config/crd/bases

setup-envtest:
	$(ENVTEST) use --bin-dir ./bin/envtest

build:
	CGO_ENABLED=0 go build -o bin/controller ./cmd/controller/...

test:
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...

lint:
	gofmt -s -l . && go vet ./...

fmt:
	gofmt -s -w .
