package auth

import (
	"crypto/md5"
	"fmt"

	"github.com/abcdlsj/gnar/pkg/proto"
)

type Authenticator interface {
	VerifyLogin(*proto.MsgLogin) bool
}

type TokenAuthenticator struct {
	token string
}

func NewTokenAuthenticator(token string) Authenticator {
	return &TokenAuthenticator{token: token}
}

func (t *TokenAuthenticator) VerifyLogin(msg *proto.MsgLogin) bool {
	hash := md5.New()
	hash.Write([]byte(t.token + fmt.Sprintf("%d", msg.Timestamp)))
	return fmt.Sprintf("%x", hash.Sum(nil)) == msg.Token
}

type Nop struct{}

func (n *Nop) VerifyLogin(msg *proto.MsgLogin) bool {
	return true
}
