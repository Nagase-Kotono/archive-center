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
upstream 업데이트를 반영할 땐 **백업 → 빌드만 → 스키마 → 검증 → 기동** 순서를 지켜요.
새 코드가 새 테이블 없이 먼저 뜨면 해당 요청만 500으로 실패해요.

```bash
git fetch upstream && git merge upstream/main

# 1) 논리 백업 + 롤백용 이미지 태그
docker exec ac-mariadb sh -c 'mariadb-dump --single-transaction --routines --events \
  -uroot -p"$MARIADB_ROOT_PASSWORD" archive_center' \
  > /Volumes/Nagase_K/archive-center/backups/archive_center-$(date +%Y%m%d-%H%M%S).sql
docker tag archive-center-kotono:local archive-center-kotono:<이전버전>-rollback

# 2) 이미지 빌드만 (아직 기동 안 함)
docker compose build ac-go

# 3) 기동. ac-schema가 마이그레이션을 적용하고 종료해야 ac-go가 떠요.
docker compose up -d --no-build

# 4) 검증
docker compose logs ac-schema | tail -20        # status ok / statements_applied 확인
docker exec pocketrisu-ts wget -qO- http://127.0.0.1:28080/ready
```

`AC_BUILD_VERSION` 도 새 버전으로 맞춰요 (표기용이라 기능에 영향은 없지만 `/version` 이 이 값을 그대로 내보내요).

**프롬프트 주의**: `prompts/` 는 호스트 바인드라 이미지의 새 프롬프트를 가려요. upstream이
`critic_system.txt` / `supervisor_system.txt` 를 바꾸면 호스트 쪽을 직접 갱신해야 반영돼요.
편집분이 있으면 덮어쓰기 전에 병합하세요.

RisuAI 플러그인 창의 "업데이트"는 **JS 어댑터**용이라 이 백엔드 컨테이너와 별개예요.
`Archive Center.js` 가 바뀌면 리수 쪽 플러그인도 따로 갱신해야 해요.

### 스키마 마이그레이션 (ac-schema 서비스)

`001_schema.sql` 은 MariaDB 볼륨이 빈 최초 1회만 적용돼서, 이미 돌던 DB는 별도 경로가 필요해요.
upstream 3.6부터 이 역할을 `mariadb-schema` 도구가 맡아요. 이 포크는 그 도구를 같은 이미지에
넣고 compose의 `ac-schema` 서비스로 기동 전에 자동 실행해요.

- **`Exited (0)` 이 정상이에요.** 계속 떠 있는 서비스가 아니라 마이그레이션만 적용하고 끝나는
  일회성 작업이라, 할 일을 마치면 종료된 채로 남아요. `docker ps` 와 `docker compose ps` 는
  실행 중인 것만 보여줘서 목록에서 사라진 것처럼 보이는데, `docker compose ps -a` 로 봐야 나와요.
  `ac-go` 가 떠 있으면 스키마 적용은 이미 성공한 거예요 (아래 의존 관계 참고).
- `--schema /app/migrations` 로 디렉터리를 주면 `.sql` 을 파일명 순서대로 전부 적용해요.
- 신규 마이그레이션은 `ADD COLUMN IF NOT EXISTS` 류 rerunnable 구문이라 매 기동마다 돌아도 안전해요.
- `ac-go` 는 `service_completed_successfully` 로 물려 있어서, 스키마 적용이 실패하면 백엔드가 아예 안 떠요.

정상 여부를 한 번에 보려면 (`status: ok` 와 `exit=0` 두 개만 보면 돼요):

```bash
docker compose ps -a ac-schema
docker logs ac-schema 2>&1 | tail -20
```

수동으로 한 번 더 돌리거나 적용 전 계획만 보고 싶으면 (`--execute` 를 빼면 dry-run 리포트):

```bash
docker compose run --rm --entrypoint /app/mariadb-schema ac-go --schema /app/migrations
```

적용 후 테이블 수 확인 (DDL은 원자적이지 않아서 중간 실패를 눈으로 봐야 해요):

```bash
docker exec ac-mariadb sh -c 'mariadb -uroot -p"$MARIADB_ROOT_PASSWORD" -N \
  -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=\"archive_center\""'
```

적용 이력:

- 3.0.1 → 3.5.0: `002_canon_pack_storage.sql` 수동 적용 (테이블 52 → 62)
- 3.5.0 → 3.9.9: `003`~`009` 를 `ac-schema` 자동 적용으로 전환

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

- **스키마 마이그레이션**: `ac-schema` 서비스가 기동 때마다 자동 적용해요. 실패하면 `ac-go` 가 뜨지 않으니, 백엔드가 안 올라오면 `docker compose logs ac-schema` 부터 보세요.
- **외장 SSD**: `/Volumes/Nagase_K/...` 바인드라, SSD 미마운트 상태로 기동하면 엉뚱한 위치에 빈 DB가 생길 수 있어요. 기동 전 마운트 확인.
- **namespace 의존**: `pocketrisu-ts` 가 재생성되면 ac 서비스도 network가 끊기니 재기동(`docker compose up -d`)이 필요해요.
- **비밀번호**: DSN에 들어가므로 `openssl rand -hex 32` 같은 DSN-safe(hex) 값을 쓰세요. `@ : / ?` 특수문자는 DSN 파싱을 깨요.
