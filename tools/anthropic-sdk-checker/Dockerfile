FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /anthropic-sdk-checker .

FROM scratch
COPY --from=build /anthropic-sdk-checker /anthropic-sdk-checker
ENTRYPOINT ["/anthropic-sdk-checker"]
