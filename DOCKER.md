# Archive Center — 단일 스택(Docker) 실행

원본 레포는 OS별 설치 패키지 방식이라 Docker 파일이 없어요. 이 포크 운영용으로
Go 백엔드 + MariaDB + ChromaDB 세 서비스를 `docker compose` 한 스택으로 묶은 파일을 추가했어요.

## 구조

```
[브라우저] ──> PocketRisu(6001)        화면
    │
    └────────> ac-go (28080)  ──내부망──> ac-mariadb(3306) / ac-chroma(8000)
       RisuAI 플러그인(JS)이 브라우저에서 Bridge URL 로 직접 호출
```

- 백엔드에 붙는 주체는 **브라우저에서 도는 RisuAI 플러그인**이에요. 서버끼리가 아니에요.
- MariaDB / ChromaDB 는 컨테이너 내부망에만 있고 외부로 안 열려요.
- 28080 은 **Tailscale IP 인터페이스에만** 바인드돼서, Tailscale 안의 기기에서만 접근돼요.
- 임베딩 벡터는 백엔드가 만들지 않아요. 외부(플러그인)에서 계산된 벡터가 들어와요.

## 최초 실행

```bash
cp .env.docker.example .env      # 그리고 값 채우기 (Tailscale IP, 비번, 토큰)
docker compose up -d --build
docker compose logs -f ac-go     # 부팅 로그 확인
```

준비 확인 (bearer token 필요):

```bash
TOKEN=$(grep AC_BEARER_TOKEN .env | cut -d= -f2)
IP=$(grep AC_HOST_IP .env | cut -d= -f2)
curl -H "Authorization: Bearer $TOKEN" http://$IP:28080/version
curl -H "Authorization: Bearer $TOKEN" http://$IP:28080/ready
```

## RisuAI 연결

1. `Archive Center.js` 를 RisuAI 플러그인으로 등록.
2. Bridge URL 을 `http://<Tailscale IP>:28080` 로 설정.
3. bearer token(`.env` 의 `AC_BEARER_TOKEN`)을 플러그인 인증 값에 입력.

## 업데이트

컨테이너는 자기 바이너리를 덮어쓰지 않아요(`AC_UPDATE_ENABLED=false`). 백엔드 업데이트는 재빌드로 해요.

```bash
git pull                          # 원본 코드 갱신
docker compose up -d --build      # 재빌드 + 교체
```

RisuAI 플러그인 창의 "업데이트"는 **JS 어댑터**를 갱신하는 거라 이 백엔드 컨테이너와 별개예요. 그건 그대로 살아있어요.

## 데이터 / 백업

- MariaDB: `/Volumes/Nagase_K/archive-center/mariadb` (정본)
- ChromaDB: `/Volumes/Nagase_K/archive-center/chroma` (벡터, MariaDB에서 재색인 가능)
- 프롬프트: `/Volumes/Nagase_K/archive-center/prompts` (편집 UI가 여기에 씀)

**백업**: 실행 중인 MariaDB 디렉토리를 그냥 파일 복사하면 손상 위험이 있어요. 논리 백업을 쓰세요:

```bash
docker exec ac-mariadb sh -c 'mariadb-dump --single-transaction -uroot -p"$MARIADB_ROOT_PASSWORD" archive_center' > backup-$(date +%Y%m%d).sql
```

`.env` 의 비밀번호도 함께 보관하세요 (분실 시 DB 접근 불가). Chroma는 MariaDB 정본에서 재색인되니 최악의 경우도 복구 경로가 있어요.

## 운영 주의 (이중 리뷰 반영)

- **스키마 마이그레이션**: `001_schema.sql`은 MariaDB 볼륨이 **빈 최초 1회만** 적용돼요. upstream이 스키마를 바꾸면 기존 볼륨엔 자동 반영이 안 되니, 그땐 수동 `ALTER` 또는 백업 후 볼륨 초기화가 필요해요.
- **외장 SSD**: 데이터가 `/Volumes/Nagase_K/...` 바인드라, **SSD가 마운트 안 된 상태로 기동하면** 엉뚱한 위치에 새 빈 DB가 생길 수 있어요. 기동 전 마운트를 확인하세요.
- **Tailscale**: `AC_HOST_IP` 인터페이스가 없으면(Tailscale 미기동) `ac-go`가 바인드 실패로 재시작 루프에 빠져요. 맥미니 재부팅 시 Tailscale이 먼저 올라오게 하거나 `restart`가 흡수하게 두되 인지하세요. `.env`의 `AC_HOST_IP`가 비면 기동 자체가 막혀요(`:?` 가드).
- **비밀번호**: DSN에 들어가므로 `openssl rand -hex 32` 같은 **DSN-safe(hex) 값**을 쓰세요. `@ : / ?` 같은 특수문자는 DSN 파싱을 깨요.
