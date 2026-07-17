# CLAUDE.md

RisuAI 장기기억 플러그인 Archive Center (Go 백엔드 + MariaDB 정본 + ChromaDB 벡터).

이 레포는 **개인 포크**예요.

- upstream: `https://github.com/Flazer31/archive-center.git` (원본)
- origin: `https://github.com/Nagase-Kotono/archive-center.git` (이 포크)

## 운영 방침

- **개인 운영용이라 upstream(Flazer31)에 PR 내지 않아요.** 자체 변경은 이 포크에만 둬요.
- upstream 업데이트는 주기적으로 추적해서 필요하면 반영해요 (`git fetch upstream && git merge upstream/main`).

## Docker 단일 스택

원본엔 Docker 파일이 없어서 이 포크에서 직접 구성했어요. 실행/업데이트/백업 상세는 `DOCKER.md` 참조.

- Go 백엔드 + MariaDB + ChromaDB를 `docker compose` 한 스택으로 (`feature/docker-stack` 브랜치).
- **리수(PocketRisu) 연동 구조 (중요)**: 리수 플러그인은 브라우저 CSP 때문에 fetch를 직접 못 하고, 리수 서버 프록시(`/proxy/plugin-main`)를 경유해 백엔드의 `127.0.0.1:28080`을 호출해요. 그래서 ac 서비스(go/mariadb/chroma)를 리수 tailscale sidecar(`pocketrisu-ts`) namespace에 공유(`network_mode: container:pocketrisu-ts`)해서 전부 127.0.0.1로 통신해요. 포트 노출·auth 없음 (namespace 내부라 리수만 접근).
- 전제: `pocketrisu-ts` 컨테이너가 먼저 실행 중이어야 해요. 리수 스택은 PocketRisu 레포의 `docker-compose.kotono.yml`.
- 데이터: `/Volumes/Nagase_K/archive-center/{mariadb,chroma,prompts}` 바인드 (외장 SSD).
- 백엔드는 임베딩을 직접 안 만들고 외부(플러그인) 벡터를 upsert/query만 해요. chromadb:1.5.9는 `/data` 자동 persist, 이미지에 python/curl이 없어 healthcheck는 bash `/dev/tcp`를 써요.

## 미해결 (upstream 이슈)

- `migrations/001_schema.sql`의 `arc_summaries`/`saga_digests` 중복 정의 + `DRAFT` 주석. upstream 원본 파일이라 여기서 고치지 않아요 (upstream 리포트 대상).

## 커밋 컨벤션

포크 자체 커밋은 `[tag]:` 대괄호 표기를 써요 (upstream은 무괄호). tag는 Conventional Commits 표준 11개(feat/fix/docs/style/refactor/perf/test/build/ci/chore/revert)만 사용하고, 설명은 한국어로 쓰되 기술 용어·코드 식별자는 영어 원문을 유지해요.
