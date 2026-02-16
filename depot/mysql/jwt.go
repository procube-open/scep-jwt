package mysql

import (
	"database/sql"
	"errors"
	"time"
)

type CreateJWTSecretInfo struct {
	Secret           string `json:"secret"`
	Type             string `json:"type"`
	Target           string `json:"target"`
	Available_Period string `json:"available_period"`
	Pending_Period   string `json:"pending_period"`
}

type GetJWTSecretInfo struct {
	Secret         string    `json:"secret"`
	Type           string    `json:"type"`
	Delete_At      time.Time `json:"delete_at"`
	Pending_Period string    `json:"pending_period"`
}

func (d *MySQLDepot) CreateJWTSecret(info CreateJWTSecretInfo) error {
	now := time.Now()
	challenge := info.Target + "\\" + info.Secret
	duration, err := time.ParseDuration(info.Available_Period)
	if err != nil {
		return err
	}
	deleteAt := now.Add(duration)
	_, err = d.db.Exec("INSERT INTO jwt_secrets (challenge, secret, target, type, created_at, delete_at, pending_period) VALUES (?, ?, ?, ?, ?, ?, ?)",
		challenge, info.Secret, info.Target, info.Type, now, deleteAt, info.Pending_Period)
	if err != nil {
		return err
	}
	return nil
}

func (d *MySQLDepot) DeleteJWTSecret(target string) error {
	_, err := d.db.Exec("DELETE FROM jwt_secrets WHERE target = ?", target)
	return err
}

func (d *MySQLDepot) GetJWTSecret(target string) (GetJWTSecretInfo, error) {
	var secret GetJWTSecretInfo
	rows, err := d.db.Query("SELECT secret, type, delete_at, pending_period FROM jwt_secrets WHERE target = ?", target)
	if err != nil {
		return secret, err
	}
	defer rows.Close()
	if !rows.Next() {
		return secret, sql.ErrNoRows
	}
	err = rows.Scan(&secret.Secret, &secret.Type, &secret.Delete_At, &secret.Pending_Period)
	return secret, err
}

func (d *MySQLDepot) CheckJWTSecretExpiration() error {
	rows, err := d.db.Query("SELECT target FROM jwt_secrets WHERE delete_at < NOW()")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var target string
		err := rows.Scan(&target)
		if err != nil {
			return err
		}
		client, err := d.GetClient(target)
		if err != nil {
			return err
		}
		switch client.JwtStatus {
		case "ISSUABLE":
			_, err = d.db.Exec("UPDATE clients SET jwt_status = 'INACTIVE' WHERE uid = ?", target)
			if err != nil {
				return err
			}
		case "UPDATABLE":
			_, err = d.db.Exec("UPDATE clients SET jwt_status = 'ISSUED' WHERE uid = ?", target)
			if err != nil {
				return err
			}
		default:
			return errors.New("client is not issuable or updatable")
		}

		err = d.DeleteJWTSecret(target)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *MySQLDepot) PutJWTToken(uid string, token string, validFrom time.Time, validTill time.Time) error {
	client, err := d.GetClient(uid)
	if err != nil {
		return err
	}
	switch client.JwtStatus {
	case "ISSUABLE":
		if err := d.UpdateJWTStatusClient(uid, "ISSUED"); err != nil {
			return err
		}
	case "UPDATABLE":
		if err := d.UpdateJWTStatusClient(uid, "PENDING"); err != nil {
			return err
		}
		secret, err := d.GetJWTSecret(uid)
		if err != nil {
			return err
		}
		duration, err := time.ParseDuration(secret.Pending_Period)
		if err != nil {
			return err
		}
		revocationDate := time.Now().Add(duration)
		_, err = d.db.Exec("UPDATE jwt_tokens SET revocation_date = ? WHERE cn = ? AND status = 'V'", revocationDate, uid)
		if err != nil {
			return err
		}
	default:
		return errors.New("client is not issuable or updatable")
	}

	_, err = d.db.Exec("INSERT INTO jwt_tokens (cn, token, status, valid_from, valid_till) VALUES (?, ?, ?, ?, ?)",
		uid, token, "V", validFrom, validTill)
	if err != nil {
		return err
	}
	if err := d.DeleteJWTSecret(uid); err != nil {
		return err
	}
	return nil
}

func (d *MySQLDepot) CheckJWTTokenRevocation() error {
	rows, err := d.db.Query("SELECT cn, id FROM jwt_tokens WHERE status = ? AND revocation_date IS NOT NULL AND revocation_date < NOW()", "V")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cn string
		var id int
		err := rows.Scan(&cn, &id)
		if err != nil {
			return err
		}
		client, err := d.GetClient(cn)
		if err != nil {
			return err
		}
		if client.JwtStatus == "PENDING" {
			_, err = d.db.Exec("UPDATE jwt_tokens SET status = 'R' WHERE id = ?", id)
			d.db.Exec("UPDATE clients SET jwt_status = 'ISSUED' WHERE uid = ?", cn)
			return err
		}
	}
	return nil
}

func (d *MySQLDepot) CheckJWTTokenExpiration() error {
	rows, err := d.db.Query("SELECT cn, id FROM jwt_tokens WHERE status = ? AND valid_till < NOW()", "V")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cn string
		var id int
		err := rows.Scan(&cn, &id)
		if err != nil {
			return err
		}
		client, err := d.GetClient(cn)
		if err != nil {
			return err
		}
		if client.JwtStatus == "ISSUED" {
			_, err = d.db.Exec("UPDATE jwt_tokens SET status = 'R' WHERE id = ?", id)
			d.db.Exec("UPDATE clients SET jwt_status = 'INACTIVE' WHERE uid = ?", cn)
			return err
		}
	}
	return nil
}
