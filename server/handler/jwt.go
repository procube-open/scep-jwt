package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/procube-open/scep/depot/mysql"
	"github.com/procube-open/scep/hook"
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

type jwtAddRequest struct {
	Token string `json:"token"`
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

		client, err := depot.GetClient(info.Uid)
		if err != nil {
			res := ErrResp{Message: "Failed to get client"}
			w.WriteHeader(http.StatusInternalServerError)
			b, _ := json.Marshal(res)
			w.Write(b)
			return
		}
		if client == nil {
			res := ErrResp{Message: "Client not found"}
			w.WriteHeader(http.StatusUnauthorized)
			b, _ := json.Marshal(res)
			w.Write(b)
			return
		}
		audience := strings.TrimSpace(client.Origin)
		if audience == "" {
			res := ErrResp{Message: "origin is required"}
			w.WriteHeader(http.StatusBadRequest)
			b, _ := json.Marshal(res)
			w.Write(b)
			return
		}

		issuer := utils.EnvString("JWT_ISSUER", "scep-jwt")
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
		if err := hook.IssueJWTHook(info.Uid, exp.Format(time.RFC3339), tokenStr); err != nil {
			res := ErrResp{Message: "Failed to execute JWT issue hook"}
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

func AddJWTHandler(depot *mysql.MySQLDepot) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(r.Body)
		var encodedToken jwtAddRequest
		err := decoder.Decode(&encodedToken)
		if err != nil {
			returnError(w, "token is required", http.StatusInternalServerError)
			return
		}
		if encodedToken.Token == "" {
			returnError(w, "No token data", http.StatusInternalServerError)
			return
		}

		decodedToken, err := url.PathUnescape(encodedToken.Token)
		if err != nil {
			returnError(w, "Failed to decode token", http.StatusInternalServerError)
			return
		}

		caPass := utils.EnvString("SCEP_CA_PASS", "")
		caCerts, _, err := depot.CA([]byte(caPass))
		if err != nil || len(caCerts) == 0 {
			returnError(w, "Failed to load CA certificate", http.StatusInternalServerError)
			return
		}

		parsedToken, err := jwt.Parse(decodedToken, func(token *jwt.Token) (interface{}, error) {
			if token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
				return nil, errors.New("unexpected signing method")
			}
			return caCerts[0].PublicKey, nil
		})
		if err != nil || !parsedToken.Valid {
			returnError(w, "Failed to verify token", http.StatusUnauthorized)
			return
		}

		claims, ok := parsedToken.Claims.(jwt.MapClaims)
		if !ok {
			returnError(w, "Failed to parse token", http.StatusInternalServerError)
			return
		}

		uid, ok := claims["sub"].(string)
		if !ok || strings.TrimSpace(uid) == "" {
			returnError(w, "Token subject is required", http.StatusInternalServerError)
			return
		}

		validTill, err := parseJWTClaimTime(claims["exp"])
		if err != nil {
			returnError(w, "Failed to parse token", http.StatusInternalServerError)
			return
		}

		validFrom, err := parseJWTClaimTime(claims["nbf"])
		if err != nil {
			validFrom, err = parseJWTClaimTime(claims["iat"])
			if err != nil {
				returnError(w, "Failed to parse token", http.StatusInternalServerError)
				return
			}
		}

		client, err := depot.GetClient(uid)
		if err != nil {
			returnError(w, "Failed to get client", http.StatusInternalServerError)
			return
		}
		if client == nil {
			returnError(w, "Client not found", http.StatusInternalServerError)
			return
		}
		if client.JwtStatus != "ISSUABLE" && client.JwtStatus != "UPDATABLE" {
			returnError(w, "Client is not in ISSUABLE or UPDATABLE state", http.StatusInternalServerError)
			return
		}

		if err := depot.PutJWTToken(uid, decodedToken, validFrom, validTill); err != nil {
			returnError(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func JWTVerifyHandler(depot *mysql.MySQLDepot) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, client, verifyErr := verifyClientCertAndGetClient(depot, r)
		if verifyErr != nil {
			returnError(w, verifyErr.Message, verifyErr.Status)
			return
		}

		hasValidToken, err := depot.HasValidJWTToken(client.Uid)
		if err != nil {
			returnError(w, "Failed to verify JWT token", http.StatusInternalServerError)
			return
		}
		if !hasValidToken {
			returnError(w, "No valid JWT token", http.StatusUnauthorized)
			return
		}

		res := ResClient{
			Uid:        client.Uid,
			Status:     client.Status,
			JwtStatus:  client.JwtStatus,
			Origin:     client.Origin,
			Attributes: client.Attributes,
		}
		b, _ := json.Marshal(res)
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

func parseJWTClaimTime(value interface{}) (time.Time, error) {
	switch v := value.(type) {
	case float64:
		return time.Unix(int64(v), 0), nil
	case int64:
		return time.Unix(v, 0), nil
	case int:
		return time.Unix(int64(v), 0), nil
	case json.Number:
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(n, 0), nil
	default:
		return time.Time{}, errors.New("invalid claim time")
	}
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
