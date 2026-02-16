package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/procube-open/scep/depot/mysql"
	"github.com/procube-open/scep/utils"

	"github.com/golang-jwt/jwt/v5"
)

type jwtIssueRequest struct {
	Uid    string `json:"uid"`
	Secret string `json:"secret"`
}

type jwtIssueResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func CreateJWTSecretHandler(depot *mysql.MySQLDepot) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := ioReadAll(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()
		var secret mysql.CreateJWTSecretInfo
		err = json.Unmarshal(body, &secret)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if stringsContainBackslash(secret.Target, secret.Secret) {
			http.Error(w, "Target contains backslash", http.StatusInternalServerError)
			return
		}
		client, err := depot.GetClient(secret.Target)
		if err != nil {
			http.Error(w, "Target not found", http.StatusInternalServerError)
			return
		}
		switch client.JwtStatus {
		case "INACTIVE":
			err = depot.UpdateJWTStatusClient(secret.Target, "ISSUABLE")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			secret.Type = "ACTIVATE"
		case "ISSUED":
			if _, err := time.ParseDuration(secret.Pending_Period); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			err = depot.UpdateJWTStatusClient(secret.Target, "UPDATABLE")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			secret.Type = "UPDATE"
		default:
			http.Error(w, "Client is not in INACTIVE or ISSUED jwt_status", http.StatusInternalServerError)
			return
		}
		err = depot.CreateJWTSecret(secret)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

func GetJWTSecretHandler(depot *mysql.MySQLDepot) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params := muxVars(r)
		secrets, err := depot.GetJWTSecret(params["CN"])
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body, err := json.Marshal(secrets)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}

func IssueJWTHandler(depot *mysql.MySQLDepot) http.HandlerFunc {
	type ErrResp struct {
		Message string `json:"message"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(r.Body)
		var info jwtIssueRequest
		err := decoder.Decode(&info)
		if err != nil {
			res := ErrResp{Message: "Failed to decode request"}
			w.WriteHeader(http.StatusInternalServerError)
			b, _ := json.Marshal(res)
			w.Write(b)
			return
		}
		secret, err := depot.GetJWTSecret(info.Uid)
		if err != nil || secret.Secret != info.Secret {
			res := ErrResp{Message: "Failed to issue token"}
			w.WriteHeader(http.StatusUnauthorized)
			b, _ := json.Marshal(res)
			w.Write(b)
			return
		}

		issuer := utils.EnvString("JWT_ISSUER", "scep-jwt")
		audience := utils.EnvString("JWT_AUDIENCE", "scep-jwt-clients")
		ttlRaw := utils.EnvString("JWT_TTL", "1h")
		ttl, err := time.ParseDuration(ttlRaw)
		if err != nil {
			res := ErrResp{Message: "Invalid JWT_TTL"}
			w.WriteHeader(http.StatusInternalServerError)
			b, _ := json.Marshal(res)
			w.Write(b)
			return
		}

		now := time.Now()
		exp := now.Add(ttl)
		jti, err := randomHex(16)
		if err != nil {
			res := ErrResp{Message: "Failed to create token"}
			w.WriteHeader(http.StatusInternalServerError)
			b, _ := json.Marshal(res)
			w.Write(b)
			return
		}

		claims := jwt.MapClaims{
			"iss": issuer,
			"aud": audience,
			"sub": info.Uid,
			"iat": now.Unix(),
			"nbf": now.Unix(),
			"exp": exp.Unix(),
			"jti": jti,
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

		caPass := utils.EnvString("SCEP_CA_PASS", "")
		_, key, err := depot.CA([]byte(caPass))
		if err != nil {
			res := ErrResp{Message: "Failed to load signing key"}
			w.WriteHeader(http.StatusInternalServerError)
			b, _ := json.Marshal(res)
			w.Write(b)
			return
		}
		tokenStr, err := token.SignedString(key)
		if err != nil {
			res := ErrResp{Message: "Failed to sign token"}
			w.WriteHeader(http.StatusInternalServerError)
			b, _ := json.Marshal(res)
			w.Write(b)
			return
		}
		if err := depot.PutJWTToken(info.Uid, tokenStr, now, exp); err != nil {
			res := ErrResp{Message: err.Error()}
			w.WriteHeader(http.StatusInternalServerError)
			b, _ := json.Marshal(res)
			w.Write(b)
			return
		}

		resp := jwtIssueResponse{Token: tokenStr, ExpiresAt: exp}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func ioReadAll(r *http.Request) ([]byte, error) {
	return io.ReadAll(r.Body)
}

func stringsContainBackslash(values ...string) bool {
	for _, v := range values {
		if strings.Contains(v, "\\") {
			return true
		}
	}
	return false
}

func muxVars(r *http.Request) map[string]string {
	return mux.Vars(r)
}
