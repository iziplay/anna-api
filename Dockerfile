FROM golang:1.25.7-trixie AS builder

# Copy dependencies source files
RUN mkdir /data
COPY go.mod /data
COPY go.sum /data

WORKDIR /data

# Download dependencies
RUN go mod download

# Add source code
COPY . .

# Build
RUN CGO_ENABLED=0 go build -o /go/bin/app ./cmd

FROM gcr.io/distroless/static-debian13
COPY --from=builder /go/bin/app /
CMD ["/app"]