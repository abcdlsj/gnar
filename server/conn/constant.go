package conn

import "github.com/google/uuid"

const (
	UuidLen = 8
)

func NewUuid() string {
	return uuid.New().String()[:UuidLen]
}
