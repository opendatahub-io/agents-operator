FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY evil-server/main.go ./evil-server/main.go
RUN CGO_ENABLED=0 go build -o /out/evil-server ./evil-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/evil-server /evil-server
EXPOSE 9999
ENTRYPOINT ["/evil-server"]
