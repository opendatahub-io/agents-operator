FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY email-server/main.go ./email-server/main.go
RUN CGO_ENABLED=0 go build -o /out/email-server ./email-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/email-server /email-server
EXPOSE 8888
ENTRYPOINT ["/email-server"]
