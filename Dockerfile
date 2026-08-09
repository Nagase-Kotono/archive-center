# syntax=docker/dockerfile:1
# Archive Center Go 백엔드 — 단일 스택(docker compose)용 이미지.
# 원본 레포엔 Dockerfile이 없어서 이 포크 운영용으로 추가한 파일.

FROM golang:1.26 AS build
WORKDIR /src/go-service
# 의존성 레이어 캐시: go.mod/go.sum 먼저 받고 download
COPY go-service/go.mod go-service/go.sum ./
RUN go mod download
COPY go-service/ ./
# 순수 Go(go-sql-driver 포함)라 CGO 없이 정적 빌드 → distroless static에 올림
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" \
    -o /out/archive-center ./cmd/archive-center-go
# 스키마 마이그레이션 도구. 3.6부터 추가 마이그레이션(003~)이 이 도구로만 적용돼서
# 같은 이미지에 넣고 compose의 ac-schema 서비스가 기동 전에 한 번 실행해요.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" \
    -o /out/mariadb-schema ./cmd/mariadb-schema

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/archive-center /app/archive-center
COPY --from=build /out/mariadb-schema /app/mariadb-schema
# 마이그레이션 SQL. mariadb-schema가 디렉터리를 받으면 파일명 순서대로 전부 적용해요.
COPY migrations/ /app/migrations/
# 프롬프트 디렉토리 (AC_PROMPT_DIR). 편집 UI를 쓰면 compose가 host 볼륨으로 덮어씀.
# 이미지 내장본은 fallback. nonroot(65532)가 쓸 수 있게 소유권 지정.
COPY --chown=65532:65532 prompts/ /app/prompts/
ENV AC_PROMPT_DIR=/app/prompts
EXPOSE 28080
ENTRYPOINT ["/app/archive-center"]
