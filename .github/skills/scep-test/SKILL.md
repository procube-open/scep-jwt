---
name: scep-test
description: SCEPサーバの証明書発行・JWT発行・verify API・失効後挙動をdocker compose上でE2E検証するための手順集。keywords: scep, jwt, verify, revoke, X-Mtls-Clientcert, audience, subject, docker compose, curl
---

# scep-test

この skill は、SCEP/JWT 機能の実動確認を短時間で再現するための検証テンプレートです。

## いつ使うか

- API 実装変更後に E2E で正しく動くか確認したいとき
- `origin` が JWT の `aud` に反映されるか確認したいとき
- `/api/cert/verify` と `/api/jwt/verify` の成功/失敗条件を確認したいとき
- クライアント失効後に verify が拒否されることを確認したいとき

## 前提

- リポジトリルート: `/workspaces/scep-jwt`
- Docker が利用可能
- `go` と `python3` が利用可能

## 検証で扱う技術要素

- `docker compose` による MySQL + SCEP Server 起動
- 管理 API (`/admin/api/*`) による client/secret/jwt-secret 操作
- ユーザ API (`/api/*`) による cert/jwt 発行と verify
- `X-Mtls-Clientcert` ヘッダへの URL エンコード済み PEM 証明書付与
- JWT payload デコードによる `aud`/`sub` 検証
- `client/revoke` 後の verify 失敗検証

## 標準フロー

### 1. 環境起動

```bash
cd /workspaces/scep-jwt
docker compose up -d --build
curl -i http://localhost:3000/admin/api/ping
```

期待結果: `200 OK`。

### 2. SCEP クライアントバイナリ作成

```bash
cd /workspaces/scep-jwt
go build -o /tmp/scepclient-test ./cmd/scepclient
```

### 3. クライアント登録と更新

```bash
BASE=http://localhost:3000
TEST_UID=test-jwt-api

curl --location "$BASE/admin/api/client/add" \
	--header 'Content-Type: application/json' \
	--data '{"uid":"'"$TEST_UID"'","origin":"example.com","attributes":{"role":"qa"}}'

curl --location "$BASE/admin/api/client/update" \
	--request PUT \
	--header 'Content-Type: application/json' \
	--data '{"uid":"'"$TEST_UID"'","origin":"auth.example.com","attributes":{"role":"qa2"}}'

curl --location "$BASE/api/client/$TEST_UID"
```

期待結果: `origin` が `auth.example.com`。

### 4. JWT 発行と claims 検証

```bash
TEST_SECRET=pass123

curl --location "$BASE/admin/api/jwt/secret/create" \
	--header 'Content-Type: application/json' \
	--data '{"secret":"'"$TEST_SECRET"'","target":"'"$TEST_UID"'","available_period":"30m","pending_period":"1h"}'

RESP=$(curl --location "$BASE/api/jwt/issue" \
	--header 'Content-Type: application/json' \
	--data '{"uid":"'"$TEST_UID"'","secret":"'"$TEST_SECRET"'"}')

echo "$RESP"
```

`aud`/`sub` 確認:

```bash
python3 - <<'PY' "$RESP"
import json, base64, sys
obj=json.loads(sys.argv[1])
token=obj["token"]
payload=token.split('.')[1]
payload += '=' * (-len(payload) % 4)
claims=json.loads(base64.urlsafe_b64decode(payload.encode()).decode())
print(claims)
print('aud_match', claims.get('aud') == 'auth.example.com')
print('sub_match', claims.get('sub') == 'test-jwt-api')
PY
```

期待結果: `aud_match True`, `sub_match True`。

### 5. verify API 成功ケース

証明書発行:

```bash
CERT_SECRET=certpass
curl --location "$BASE/admin/api/secret/create" \
	--header 'Content-Type: application/json' \
	--data '{"secret":"'"$CERT_SECRET"'","target":"'"$TEST_UID"'","available_period":"30m","pending_period":"1h"}'

/tmp/scepclient-test -uid="$TEST_UID" -secret="$CERT_SECRET"
```

ヘッダ作成と verify:

```bash
ENC_CERT=$(python3 -c "import urllib.parse, pathlib; print(urllib.parse.quote(pathlib.Path('/workspaces/scep-jwt/cert.pem').read_text(), safe=''))")

curl -i --location "$BASE/api/cert/verify" --header "X-Mtls-Clientcert: $ENC_CERT"
curl -i --location "$BASE/api/jwt/verify"  --header "X-Mtls-Clientcert: $ENC_CERT"
```

期待結果: どちらも `200`。

### 6. verify API 失敗ケース

#### (A) ヘッダなし

```bash
curl -i --location "$BASE/api/jwt/verify"
```

期待結果: `500` + `{"message":"No Certificate"}`。

#### (B) 失効後

```bash
curl --location "$BASE/admin/api/client/revoke" \
	--header 'Content-Type: application/json' \
	--data '{"uid":"'"$TEST_UID"'"}'

curl -i --location "$BASE/api/cert/verify" --header "X-Mtls-Clientcert: $ENC_CERT"
curl -i --location "$BASE/api/jwt/verify"  --header "X-Mtls-Clientcert: $ENC_CERT"
```

期待結果: どちらも `401` + `{"message":"Certificate is revoked"}`。

## 判定チェックリスト

- `origin` 更新値が `/api/client/{uid}` に反映される
- JWT `aud` が `origin` と一致する
- JWT `sub` が `uid` と一致する
- `origin` 空で `/api/jwt/issue` が失敗する（`400`）
- 証明書ヘッダありで verify が成功する
- 失効後は verify が失敗する

## クリーンアップ

```bash
cd /workspaces/scep-jwt
docker compose down -v
rm -f /workspaces/scep-jwt/cert.pem /workspaces/scep-jwt/key.pem /workspaces/scep-jwt/csr.pem
```