FROM node:22-alpine AS frontend
WORKDIR /build/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /build/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /build/household-bills ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S ledger && adduser -S -G ledger ledger
WORKDIR /app
COPY --from=backend /build/household-bills ./household-bills
COPY --from=frontend /build/frontend/dist ./public
USER ledger
ENV LISTEN_ADDR=:8000 FRONTEND_DIST=/app/public
EXPOSE 8000
CMD ["./household-bills"]
