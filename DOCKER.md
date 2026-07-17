# Archive Center — 단일 스택(Docker) 실행

원본 레포는 OS별 설치 패키지 방식이라 Docker 파일이 없어요. 이 포크 운영용으로
Go 백엔드 + MariaDB + ChromaDB 세 서비스를 docker compose 한 스택으로 묶고,
PocketRisu(리수) 컨테이너와 network namespace를 공유하도록 구성했어요.

## 연동 구조 (중요)

리수 플러그인은 브라우저 CSP 때문에 fetch를 직접 못 하고, **리수 서버를 프록시 경유**해서
백엔드를 호출해요. 그 프록시 대상이 리수 컨테이너 자신의 `127.0.0.1:28080` 이에요.

```
[브라우저] → 리수 플러그인(JS)
   → risuai.nativeFetch → 리수 서버 /proxy/plugin-main → 127.0.0.1:28080
   → ac-go (리수와 같은 namespace) ──127.0.0.1──> mariadb:3306 / chroma:8000
```

그래서 ac 서비스들을 리수의 tailscale sidecar(`pocketrisu-ts`) namespace에 붙여서
(`network_mode: container:pocketrisu-ts`) 전부 127.0.0.1 로 통신해요. 포트는 호스트에
노출하지 않고, auth도 끕니다 (namespace 내부라 리수만 접근).

**전제: `pocketrisu-ts` 컨테이너가 먼저 실행 중이어야 해요** (리수 kotono 스택).

## 최초 실행

리수(kotono) 스택이 먼저 떠 있어야 해요. 그 다음:

```bash
cp .env.docker.example .env      # MARIADB 비밀번호 채우기
docker compose up -d --build
docker compose logs -f ac-go
```

준비 확인 (리수 namespace 안에서, auth 없음):

```bash
docker exec pocketrisu-ts wget -qO- http://127.0.0.1:28080/ready
```

## RisuAI 연결

1. `Archive Center.js` 를 RisuAI 플러그인으로 등록.
2. Bridge URL 은 **기본값 `http://127.0.0.1:28080` 그대로** (리수 프록시가 같은 namespace에서 붙어요).
3. auth를 껐으니 **토큰 입력 불필요**.

## 업데이트

컨테이너는 자기 바이너리를 덮어쓰지 않아요(`AC_UPDATE_ENABLED=false`). 재빌드로 업데이트해요.

```bash
git pull
docker compose up -d --build
```

RisuAI 플러그인 창의 "업데이트"는 **JS 어댑터**용이라 이 백엔드 컨테이너와 별개예요.

## 데이터 / 백업

- MariaDB: `/Volumes/Nagase_K/archive-center/mariadb` (정본)
- ChromaDB: `/Volumes/Nagase_K/archive-center/chroma` (벡터, MariaDB에서 재색인 가능)
- 프롬프트: `/Volumes/Nagase_K/archive-center/prompts` (편집 UI가 여기에 씀)

**백업**: 실행 중인 MariaDB 디렉토리를 그냥 복사하면 손상 위험이 있어요. 논리 백업:

```bash
docker exec ac-mariadb sh -c 'mariadb-dump --single-transaction -uroot -p"$MARIADB_ROOT_PASSWORD" archive_center' > backup-$(date +%Y%m%d).sql
```

`.env` 의 비밀번호도 함께 보관하세요. Chroma는 MariaDB 정본에서 재색인되니 복구 경로가 있어요.

## 운영 주의 (이중 리뷰 반영)

- **스키마 마이그레이션**: `001_schema.sql`은 MariaDB 볼륨이 빈 최초 1회만 적용돼요. upstream이 스키마를 바꾸면 수동 `ALTER` 또는 백업 후 볼륨 초기화가 필요해요.
- **외장 SSD**: `/Volumes/Nagase_K/...` 바인드라, SSD 미마운트 상태로 기동하면 엉뚱한 위치에 빈 DB가 생길 수 있어요. 기동 전 마운트 확인.
- **namespace 의존**: `pocketrisu-ts` 가 재생성되면 ac 서비스도 network가 끊기니 재기동(`docker compose up -d`)이 필요해요.
- **비밀번호**: DSN에 들어가므로 `openssl rand -hex 32` 같은 DSN-safe(hex) 값을 쓰세요. `@ : / ?` 특수문자는 DSN 파싱을 깨요.
