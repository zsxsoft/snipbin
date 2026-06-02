FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn go mod download
COPY main.go ./
COPY web/ ./web/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/snipbin ./main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/snipbin /snipbin
EXPOSE 8080
ENTRYPOINT ["/snipbin"]
