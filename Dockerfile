FROM golang:1.26-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath \
    -ldflags "-s -w -X 'github.com/samn/gke-cost-analyzer/cmd.version=${VERSION}'" \
    -o /gke-cost-analyzer .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /gke-cost-analyzer /usr/local/bin/gke-cost-analyzer
ENTRYPOINT ["gke-cost-analyzer"]
