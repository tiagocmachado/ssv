#
# STEP 1: Prepare environment
#
FROM golang:1.15 AS preparer

RUN apt-get update                                                        && \
  DEBIAN_FRONTEND=noninteractive apt-get install -yq --no-install-recommends \
    curl git zip unzip wget g++ python gcc-aarch64-linux-gnu                 \
  && rm -rf /var/lib/apt/lists/*

RUN go version
RUN python --version

WORKDIR /go/src/github.com/bloxapp/ssv/
COPY go.mod .
COPY go.sum .
RUN go mod download

#
# STEP 2: Build executable binary
#
FROM preparer AS builder

# Copy files and install app
COPY . .

RUN go get -d -v ./...

RUN GOOS=linux go install -tags blst_enabled -ldflags "-X main.Version=`git describe --tags $(git rev-list --tags --max-count=1)` -linkmode external -extldflags \"-static -lm\"" ./cmd/ssvnode

#
# STEP 3: Prepare image to run the binary
#
FROM ubuntu:20.04 AS runner

# Install ca-certificates, make
RUN apt-get update && apt-get install --no-install-recommends -y \
    ca-certificates make bash \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /go/bin/ssvnode /go/bin/ssvnode
COPY ./Makefile .env* ./
COPY config/* ./config/


# Expose port for load balancing
EXPOSE 5678 5000 4000/udp

#ENTRYPOINT ["/go/bin/ssvnode"]
