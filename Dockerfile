FROM golang:1.26-alpine AS build
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /gomini ./cmd/gomini

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /gomini /gomini
COPY config.example.json /etc/gomini/config.json
EXPOSE 8081
ENTRYPOINT ["/gomini"]
CMD ["--config", "/etc/gomini/config.json"]
