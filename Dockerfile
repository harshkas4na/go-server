# STAGE 1: Build
FROM golang:1.26-alpine AS builder

WORKDIR /app

# 1. Copy only the dependency files first
COPY go.mod go.sum ./
RUN go mod download

# 2. Copy the rest of the source code
COPY . .

# 3. Build
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

# STAGE 2: Final Image
FROM alpine:latest  

# Added ca-certificates in case your app ever needs to talk to HTTPS APIs
RUN apk --no-cache add ca-certificates

WORKDIR /root/
COPY --from=builder /app/server .

EXPOSE 8080
CMD ["./server"]