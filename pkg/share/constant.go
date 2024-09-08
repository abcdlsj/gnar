package share

import "fmt"

var (
	GitHash    string
	BuildStamp string
)

func GetVersion() string {
	return fmt.Sprintf("v0.0.1-%s", GitHash)
}
