package auth

import (
	"crypto/subtle"
	"errors"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUnauthorized       = errors.New("unauthorized")
)

// AdminCredentials 来自配置的单一管理员账号。
type AdminCredentials struct {
	Username string
	Password string
}

// Authenticator 校验管理员账号密码。
type Authenticator struct {
	creds AdminCredentials
}

func NewAuthenticator(creds AdminCredentials) *Authenticator {
	return &Authenticator{creds: creds}
}

// Authenticate 成功返回规范化用户名。
func (a *Authenticator) Authenticate(username, password string) (string, error) {
	uOK := subtle.ConstantTimeCompare([]byte(username), []byte(a.creds.Username)) == 1
	pOK := subtle.ConstantTimeCompare([]byte(password), []byte(a.creds.Password)) == 1
	if !uOK || !pOK {
		return "", ErrInvalidCredentials
	}
	return a.creds.Username, nil
}
